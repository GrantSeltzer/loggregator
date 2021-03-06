// This file was generated by github.com/nelsam/hel.  Do not
// edit this code by hand unless you *really* know what you're
// doing.  Expect any changes made manually to be overwritten
// the next time hel regenerates this file.

package batch_test

type mockBatchCounterIncrementer struct {
	BatchIncrementCounterCalled chan bool
	BatchIncrementCounterInput  struct {
		Name chan string
	}
}

func newMockBatchCounterIncrementer() *mockBatchCounterIncrementer {
	m := &mockBatchCounterIncrementer{}
	m.BatchIncrementCounterCalled = make(chan bool, 100)
	m.BatchIncrementCounterInput.Name = make(chan string, 100)
	return m
}
func (m *mockBatchCounterIncrementer) BatchIncrementCounter(name string) {
	m.BatchIncrementCounterCalled <- true
	m.BatchIncrementCounterInput.Name <- name
}
