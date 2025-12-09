package network_variable

func defaultOptions() options {
	return options{
		bufferSize: 1024,
	}
}

type Option func(*options)

type options struct {
	bufferSize int
}

func WithBufferSize(size int) Option {
	return func(o *options) {
		o.bufferSize = size
	}
}
