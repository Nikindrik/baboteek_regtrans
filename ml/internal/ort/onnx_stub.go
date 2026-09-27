//go:build !onnxruntime

package ort

import "fmt"

type stubRunner struct{}

func New(modelPath, inputName, outputName string) (Runner, error) {
	return nil, fmt.Errorf("ONNX Runtime support is not compiled in; build with CGO_ENABLED=1 go build -tags onnxruntime and install third_party/onnxruntime")
}
func (s *stubRunner) Run([]float32) ([]float32, error) {
	return nil, fmt.Errorf("ONNX Runtime unavailable")
}
func (s *stubRunner) Close() error { return nil }
