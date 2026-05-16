package msisdn

type Runner interface {
	Run(data []byte) error
	Select() ([]byte, error)
}

type commandRunner interface {
	Run(command []byte) ([]byte, error)
}
