// Code generated by counterfeiter. DO NOT EDIT.
package mock

import (
	"sync"

	"github.com/hyperledger/fabric/core/container/ccintf"
	"github.com/hyperledger/fabric/core/scc"
)

type ChaincodeStreamHandler struct {
	HandleChaincodeStreamStub        func(ccintf.ChaincodeStream) error
	handleChaincodeStreamMutex       sync.RWMutex
	handleChaincodeStreamArgsForCall []struct {
		arg1 ccintf.ChaincodeStream
	}
	handleChaincodeStreamReturns struct {
		result1 error
	}
	handleChaincodeStreamReturnsOnCall map[int]struct {
		result1 error
	}
	LaunchInProcStub        func(ccintf.CCID) <-chan struct{}
	launchInProcMutex       sync.RWMutex
	launchInProcArgsForCall []struct {
		arg1 ccintf.CCID
	}
	launchInProcReturns struct {
		result1 <-chan struct{}
	}
	launchInProcReturnsOnCall map[int]struct {
		result1 <-chan struct{}
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *ChaincodeStreamHandler) HandleChaincodeStream(arg1 ccintf.ChaincodeStream) error {
	fake.handleChaincodeStreamMutex.Lock()
	ret, specificReturn := fake.handleChaincodeStreamReturnsOnCall[len(fake.handleChaincodeStreamArgsForCall)]
	fake.handleChaincodeStreamArgsForCall = append(fake.handleChaincodeStreamArgsForCall, struct {
		arg1 ccintf.ChaincodeStream
	}{arg1})
	fake.recordInvocation("HandleChaincodeStream", []interface{}{arg1})
	fake.handleChaincodeStreamMutex.Unlock()
	if fake.HandleChaincodeStreamStub != nil {
		return fake.HandleChaincodeStreamStub(arg1)
	}
	if specificReturn {
		return ret.result1
	}
	fakeReturns := fake.handleChaincodeStreamReturns
	return fakeReturns.result1
}

func (fake *ChaincodeStreamHandler) HandleChaincodeStreamCallCount() int {
	fake.handleChaincodeStreamMutex.RLock()
	defer fake.handleChaincodeStreamMutex.RUnlock()
	return len(fake.handleChaincodeStreamArgsForCall)
}

func (fake *ChaincodeStreamHandler) HandleChaincodeStreamCalls(stub func(ccintf.ChaincodeStream) error) {
	fake.handleChaincodeStreamMutex.Lock()
	defer fake.handleChaincodeStreamMutex.Unlock()
	fake.HandleChaincodeStreamStub = stub
}

func (fake *ChaincodeStreamHandler) HandleChaincodeStreamArgsForCall(i int) ccintf.ChaincodeStream {
	fake.handleChaincodeStreamMutex.RLock()
	defer fake.handleChaincodeStreamMutex.RUnlock()
	argsForCall := fake.handleChaincodeStreamArgsForCall[i]
	return argsForCall.arg1
}

func (fake *ChaincodeStreamHandler) HandleChaincodeStreamReturns(result1 error) {
	fake.handleChaincodeStreamMutex.Lock()
	defer fake.handleChaincodeStreamMutex.Unlock()
	fake.HandleChaincodeStreamStub = nil
	fake.handleChaincodeStreamReturns = struct {
		result1 error
	}{result1}
}

func (fake *ChaincodeStreamHandler) HandleChaincodeStreamReturnsOnCall(i int, result1 error) {
	fake.handleChaincodeStreamMutex.Lock()
	defer fake.handleChaincodeStreamMutex.Unlock()
	fake.HandleChaincodeStreamStub = nil
	if fake.handleChaincodeStreamReturnsOnCall == nil {
		fake.handleChaincodeStreamReturnsOnCall = make(map[int]struct {
			result1 error
		})
	}
	fake.handleChaincodeStreamReturnsOnCall[i] = struct {
		result1 error
	}{result1}
}

func (fake *ChaincodeStreamHandler) LaunchInProc(arg1 ccintf.CCID) <-chan struct{} {
	fake.launchInProcMutex.Lock()
	ret, specificReturn := fake.launchInProcReturnsOnCall[len(fake.launchInProcArgsForCall)]
	fake.launchInProcArgsForCall = append(fake.launchInProcArgsForCall, struct {
		arg1 ccintf.CCID
	}{arg1})
	fake.recordInvocation("LaunchInProc", []interface{}{arg1})
	fake.launchInProcMutex.Unlock()
	if fake.LaunchInProcStub != nil {
		return fake.LaunchInProcStub(arg1)
	}
	if specificReturn {
		return ret.result1
	}
	fakeReturns := fake.launchInProcReturns
	return fakeReturns.result1
}

func (fake *ChaincodeStreamHandler) LaunchInProcCallCount() int {
	fake.launchInProcMutex.RLock()
	defer fake.launchInProcMutex.RUnlock()
	return len(fake.launchInProcArgsForCall)
}

func (fake *ChaincodeStreamHandler) LaunchInProcCalls(stub func(ccintf.CCID) <-chan struct{}) {
	fake.launchInProcMutex.Lock()
	defer fake.launchInProcMutex.Unlock()
	fake.LaunchInProcStub = stub
}

func (fake *ChaincodeStreamHandler) LaunchInProcArgsForCall(i int) ccintf.CCID {
	fake.launchInProcMutex.RLock()
	defer fake.launchInProcMutex.RUnlock()
	argsForCall := fake.launchInProcArgsForCall[i]
	return argsForCall.arg1
}

func (fake *ChaincodeStreamHandler) LaunchInProcReturns(result1 <-chan struct{}) {
	fake.launchInProcMutex.Lock()
	defer fake.launchInProcMutex.Unlock()
	fake.LaunchInProcStub = nil
	fake.launchInProcReturns = struct {
		result1 <-chan struct{}
	}{result1}
}

func (fake *ChaincodeStreamHandler) LaunchInProcReturnsOnCall(i int, result1 <-chan struct{}) {
	fake.launchInProcMutex.Lock()
	defer fake.launchInProcMutex.Unlock()
	fake.LaunchInProcStub = nil
	if fake.launchInProcReturnsOnCall == nil {
		fake.launchInProcReturnsOnCall = make(map[int]struct {
			result1 <-chan struct{}
		})
	}
	fake.launchInProcReturnsOnCall[i] = struct {
		result1 <-chan struct{}
	}{result1}
}

func (fake *ChaincodeStreamHandler) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.handleChaincodeStreamMutex.RLock()
	defer fake.handleChaincodeStreamMutex.RUnlock()
	fake.launchInProcMutex.RLock()
	defer fake.launchInProcMutex.RUnlock()
	copiedInvocations := map[string][][]interface{}{}
	for key, value := range fake.invocations {
		copiedInvocations[key] = value
	}
	return copiedInvocations
}

func (fake *ChaincodeStreamHandler) recordInvocation(key string, args []interface{}) {
	fake.invocationsMutex.Lock()
	defer fake.invocationsMutex.Unlock()
	if fake.invocations == nil {
		fake.invocations = map[string][][]interface{}{}
	}
	if fake.invocations[key] == nil {
		fake.invocations[key] = [][]interface{}{}
	}
	fake.invocations[key] = append(fake.invocations[key], args)
}

var _ scc.ChaincodeStreamHandler = new(ChaincodeStreamHandler)
