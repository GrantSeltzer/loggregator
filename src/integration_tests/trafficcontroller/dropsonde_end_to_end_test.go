package trafficcontroller_test

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"plumbing"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"integration_tests/trafficcontroller/fake_doppler"

	"github.com/cloudfoundry/noaa/consumer"
	"github.com/cloudfoundry/sonde-go/events"
)

var _ = Describe("TrafficController for dropsonde messages", func() {
	var dropsondeEndpoint string

	BeforeEach(func() {
		fakeDoppler = fake_doppler.New()
		go fakeDoppler.Start()
		dropsondeEndpoint = fmt.Sprintf("ws://%s:%d", localIPAddress, TRAFFIC_CONTROLLER_DROPSONDE_PORT)
	})

	AfterEach(func() {
		fakeDoppler.Stop()
	})

	FContext("Streaming", func() {
		var (
			client   *consumer.Consumer
			messages <-chan *events.Envelope
			errors   <-chan error
		)

		JustBeforeEach(func() {
			client = consumer.New(dropsondeEndpoint, &tls.Config{}, nil)
			messages, errors = client.StreamWithoutReconnect(APP_ID, AUTH_TOKEN)
		})

		It("passes messages through", func() {
			time.Sleep(10 * time.Second)
			var request *http.Request
			Eventually(fakeDoppler.TrafficControllerConnected, 10).Should(Receive(&request))
			Expect(request.URL.Path).To(Equal("/apps/1234/stream"))

			currentTime := time.Now().UnixNano()
			dropsondeMessage := makeDropsondeMessage("Hello through NOAA", APP_ID, currentTime)
			fakeDoppler.SendLogMessageViaGrpc(dropsondeMessage)

			var grpcRequest *plumbing.StreamRequest
			Eventually(fakeDoppler.GrpcStreamRequestChan).Should(Receive(&grpcRequest))
			Expect(grpcRequest.AppID).To(Equal(APP_ID))

			var receivedEnvelope *events.Envelope
			Eventually(messages).Should(Receive(&receivedEnvelope))
			Consistently(errors).ShouldNot(Receive())

			receivedMessage := receivedEnvelope.GetLogMessage()
			Expect(receivedMessage.GetMessage()).To(BeEquivalentTo("Hello through NOAA"))
			Expect(receivedMessage.GetAppId()).To(Equal(APP_ID))
			Expect(receivedMessage.GetTimestamp()).To(Equal(currentTime))

			client.Close()
		})

		It("closes the upstream websocket connection when done", func() {
			var request *http.Request
			Eventually(fakeDoppler.TrafficControllerConnected, 10).Should(Receive(&request))
			Eventually(fakeDoppler.ConnectionPresent).Should(BeTrue())

			client.Close()

			Eventually(fakeDoppler.ConnectionPresent).Should(BeFalse())
		})
	})

	Context("Firehose", func() {
		var (
			messages <-chan *events.Envelope
			errors   <-chan error
		)

		It("passes messages through for every app for uaa admins", func() {
			client := consumer.New(dropsondeEndpoint, &tls.Config{}, nil)
			messages, errors = client.FirehoseWithoutReconnect(SUBSCRIPTION_ID, AUTH_TOKEN)

			var request *http.Request
			Eventually(fakeDoppler.TrafficControllerConnected, 10).Should(Receive(&request))
			Expect(request.URL.Path).To(Equal("/firehose/" + SUBSCRIPTION_ID))

			currentTime := time.Now().UnixNano()
			dropsondeMessage := makeDropsondeMessage("Hello through NOAA", APP_ID, currentTime)
			fakeDoppler.SendLogMessage(dropsondeMessage)

			var receivedEnvelope *events.Envelope
			Eventually(messages).Should(Receive(&receivedEnvelope))
			Consistently(errors).ShouldNot(Receive())

			receivedMessage := receivedEnvelope.GetLogMessage()
			Expect(receivedMessage.GetMessage()).To(BeEquivalentTo("Hello through NOAA"))
			Expect(receivedMessage.GetAppId()).To(Equal(APP_ID))
			Expect(receivedMessage.GetTimestamp()).To(Equal(currentTime))

			client.Close()
		})
	})

	Context("Recent", func() {
		var expectedMessages [][]byte

		BeforeEach(func() {
			expectedMessages = make([][]byte, 5)

			for i := 0; i < 5; i++ {
				message := makeDropsondeMessage(strconv.Itoa(i), "1234", 1234)
				expectedMessages[i] = message
				fakeDoppler.SendLogMessage(message)
			}
			fakeDoppler.CloseLogMessageStream()
		})

		It("returns a multi-part HTTP response with all recent messages", func() {
			client := consumer.New(dropsondeEndpoint, &tls.Config{}, nil)

			Eventually(func() bool {
				messages, err := client.RecentLogs("1234", "bearer iAmAnAdmin")
				Expect(err).NotTo(HaveOccurred())
				select {
				case request := <-fakeDoppler.TrafficControllerConnected:
					Expect(request.URL.Path).To(Equal("/apps/1234/recentlogs"))
					for i, message := range messages {
						Expect(message.GetMessage()).To(BeEquivalentTo(strconv.Itoa(i)))
					}
					return true
				default:
					return false
				}
			}, 5).Should(BeTrue())
		})
	})

	Context("ContainerMetrics", func() {
		BeforeEach(func() {
			for i := 0; i < 5; i++ {
				message := makeContainerMetricMessage("appID", int32(i), float64(i), 100000)
				fakeDoppler.SendLogMessage(message)
			}

			oldmessage := makeContainerMetricMessage("appID", 1, 6, 50000)
			fakeDoppler.SendLogMessage(oldmessage)

			fakeDoppler.CloseLogMessageStream()
		})

		It("returns a multi-part HTTP response with the most recent message for all instances for a given app", func() {
			client := consumer.New(dropsondeEndpoint, &tls.Config{}, nil)

			Eventually(func() bool {
				messages, err := client.ContainerMetrics("1234", "bearer iAmAnAdmin")
				Expect(err).NotTo(HaveOccurred())

				select {
				case request := <-fakeDoppler.TrafficControllerConnected:
					Expect(request.URL.Path).To(Equal("/apps/1234/containermetrics"))
					for i, message := range messages {
						Expect(message.GetInstanceIndex()).To(BeEquivalentTo(i))
						Expect(message.GetCpuPercentage()).To(BeEquivalentTo(i))
					}
					return true
				default:
					return false
				}
			}, 5).Should(BeTrue())
		})
	})

	Context("SetCookie", func() {
		It("sets the desired cookie on the response", func() {
			response, err := http.PostForm(fmt.Sprintf("http://%s:%d/set-cookie", localIPAddress, TRAFFIC_CONTROLLER_DROPSONDE_PORT), url.Values{"CookieName": {"authorization"}, "CookieValue": {url.QueryEscape("bearer iAmAnAdmin")}})
			Expect(err).NotTo(HaveOccurred())

			Expect(response.Cookies()).NotTo(BeNil())
			Expect(response.Cookies()).To(HaveLen(1))
			cookie := response.Cookies()[0]
			Expect(cookie.Domain).To(Equal("doppler.vcap.me"))
			Expect(cookie.Name).To(Equal("authorization"))
			Expect(cookie.Value).To(Equal("bearer+iAmAnAdmin"))
			Expect(cookie.Secure).To(BeTrue())
		})
	})
})
