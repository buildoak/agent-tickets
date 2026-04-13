package dispatch

// MockDispatcher implements Dispatcher for testing.
type MockDispatcher struct {
	DispatchFunc func(opts DispatchOptions) (*DispatchResult, error)
	StatusFunc   func(dispatchID string) (*StatusResult, error)
}

func (m *MockDispatcher) Dispatch(opts DispatchOptions) (*DispatchResult, error) {
	if m.DispatchFunc != nil {
		return m.DispatchFunc(opts)
	}

	// Default mimics the async_started response from agent-mux --async,
	// which does NOT include session_id.
	return &DispatchResult{DispatchID: "mock-dispatch-id"}, nil
}

func (m *MockDispatcher) Status(dispatchID string) (*StatusResult, error) {
	if m.StatusFunc != nil {
		return m.StatusFunc(dispatchID)
	}

	return &StatusResult{Status: "completed"}, nil
}
