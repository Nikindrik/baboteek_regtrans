package ort

type Runner interface {
	Run([]float32) ([]float32, error)
	Close() error
}
