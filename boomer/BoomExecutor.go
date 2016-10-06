package boomer

type BoomExecutor interface {
	Execute(results chan *Result)

	// Used for printing results
	GetHost() string
	GetEndpoint() string
	GetRequestType() string
}