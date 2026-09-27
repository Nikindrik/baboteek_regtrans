//go:build onnxruntime && cgo

package ort

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/onnxruntime/include
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/onnxruntime/lib -lonnxruntime -Wl,-rpath,${SRCDIR}/../../third_party/onnxruntime/lib
#include <stdlib.h>
#include <string.h>
#include "onnxruntime_c_api.h"

typedef struct {
    const OrtApi* api;
    OrtEnv* env;
    OrtSessionOptions* opts;
    OrtSession* session;
    OrtMemoryInfo* mem;
} MgtOrtRunner;

static char* mgt_strdup(const char* s) {
    if (!s) return NULL;
    size_t n = strlen(s) + 1;
    char* p = (char*)malloc(n);
    if (p) memcpy(p, s, n);
    return p;
}

static int mgt_status(MgtOrtRunner* r, OrtStatus* st, char** err) {
    if (!st) return 0;
    const char* msg = r && r->api ? r->api->GetErrorMessage(st) : "ONNX Runtime error";
    *err = mgt_strdup(msg);
    if (r && r->api) r->api->ReleaseStatus(st);
    return -1;
}

static int mgt_create(const char* model_path, MgtOrtRunner** out, char** err) {
    *out = NULL; *err = NULL;
    MgtOrtRunner* r = (MgtOrtRunner*)calloc(1, sizeof(MgtOrtRunner));
    if (!r) { *err = mgt_strdup("out of memory"); return -1; }
    r->api = OrtGetApiBase()->GetApi(ORT_API_VERSION);
    if (!r->api) { *err = mgt_strdup("OrtGetApi failed"); free(r); return -1; }
    if (mgt_status(r, r->api->CreateEnv(ORT_LOGGING_LEVEL_WARNING, "mosgortrans", &r->env), err)) goto fail;
    if (mgt_status(r, r->api->CreateSessionOptions(&r->opts), err)) goto fail;
    if (mgt_status(r, r->api->SetIntraOpNumThreads(r->opts, 1), err)) goto fail;
    if (mgt_status(r, r->api->SetInterOpNumThreads(r->opts, 1), err)) goto fail;
    if (mgt_status(r, r->api->SetSessionGraphOptimizationLevel(r->opts, ORT_ENABLE_ALL), err)) goto fail;
    if (mgt_status(r, r->api->CreateSession(r->env, model_path, r->opts, &r->session), err)) goto fail;
    if (mgt_status(r, r->api->CreateCpuMemoryInfo(OrtArenaAllocator, OrtMemTypeDefault, &r->mem), err)) goto fail;
    *out = r; return 0;
fail:
    if (r->mem) r->api->ReleaseMemoryInfo(r->mem);
    if (r->session) r->api->ReleaseSession(r->session);
    if (r->opts) r->api->ReleaseSessionOptions(r->opts);
    if (r->env) r->api->ReleaseEnv(r->env);
    free(r); return -1;
}

static void mgt_release(MgtOrtRunner* r) {
    if (!r) return;
    if (r->mem) r->api->ReleaseMemoryInfo(r->mem);
    if (r->session) r->api->ReleaseSession(r->session);
    if (r->opts) r->api->ReleaseSessionOptions(r->opts);
    if (r->env) r->api->ReleaseEnv(r->env);
    free(r);
}

static int mgt_run(MgtOrtRunner* r, const char* input_name, const char* output_name,
                   float* input, size_t input_count, float** output, size_t* output_count, char** err) {
    *output = NULL; *output_count = 0; *err = NULL;
    int64_t shape[2] = {1, (int64_t)input_count};
    OrtValue* in = NULL; OrtValue* out = NULL; OrtTensorTypeAndShapeInfo* info = NULL;
    if (mgt_status(r, r->api->CreateTensorWithDataAsOrtValue(r->mem, input, input_count*sizeof(float), shape, 2, ONNX_TENSOR_ELEMENT_DATA_TYPE_FLOAT, &in), err)) goto fail;
    const char* input_names[1] = {input_name}; const char* output_names[1] = {output_name};
    if (mgt_status(r, r->api->Run(r->session, NULL, input_names, (const OrtValue* const*)&in, 1, output_names, 1, &out), err)) goto fail;
    int is_tensor = 0; if (mgt_status(r, r->api->IsTensor(out, &is_tensor), err)) goto fail; if (!is_tensor) { *err=mgt_strdup("requested ONNX output is not a tensor"); goto fail; }
    if (mgt_status(r, r->api->GetTensorTypeAndShape(out, &info), err)) goto fail;
    size_t n = 0; if (mgt_status(r, r->api->GetTensorShapeElementCount(info, &n), err)) goto fail;
    ONNXTensorElementDataType typ; if (mgt_status(r, r->api->GetTensorElementType(info, &typ), err)) goto fail;
    float* dst = (float*)malloc(n*sizeof(float)); if (!dst) { *err=mgt_strdup("out of memory"); goto fail; }
    void* data = NULL; if (mgt_status(r, r->api->GetTensorMutableData(out, &data), err)) { free(dst); goto fail; }
    if (typ == ONNX_TENSOR_ELEMENT_DATA_TYPE_FLOAT) { memcpy(dst, data, n*sizeof(float)); }
    else if (typ == ONNX_TENSOR_ELEMENT_DATA_TYPE_DOUBLE) { double* p=(double*)data; for(size_t i=0;i<n;i++) dst[i]=(float)p[i]; }
    else { free(dst); *err=mgt_strdup("ONNX output must be float/double tensor"); goto fail; }
    *output=dst; *output_count=n;
    if (info) r->api->ReleaseTensorTypeAndShapeInfo(info); if (out) r->api->ReleaseValue(out); if (in) r->api->ReleaseValue(in); return 0;
fail:
    if (info) r->api->ReleaseTensorTypeAndShapeInfo(info); if (out) r->api->ReleaseValue(out); if (in) r->api->ReleaseValue(in); return -1;
}
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"math"
	"unsafe"
)

type cgoRunner struct {
	ptr        *C.MgtOrtRunner
	inputName  *C.char
	outputName *C.char
}

func New(modelPath, inputName, outputName string) (Runner, error) {
	cp := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cp))
	cin := C.CString(inputName)
	cout := C.CString(outputName)
	var ptr *C.MgtOrtRunner
	var cerr *C.char
	if C.mgt_create(cp, &ptr, &cerr) != 0 {
		if cin != nil {
			C.free(unsafe.Pointer(cin))
		}
		if cout != nil {
			C.free(unsafe.Pointer(cout))
		}
		msg := "ONNX Runtime create failed"
		if cerr != nil {
			msg = C.GoString(cerr)
			C.free(unsafe.Pointer(cerr))
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return &cgoRunner{ptr: ptr, inputName: cin, outputName: cout}, nil
}
func (r *cgoRunner) Run(x []float32) ([]float32, error) {
	if len(x) == 0 {
		return nil, fmt.Errorf("empty input")
	}
	var out *C.float
	var n C.size_t
	var cerr *C.char
	rc := C.mgt_run(r.ptr, r.inputName, r.outputName, (*C.float)(unsafe.Pointer(&x[0])), C.size_t(len(x)), &out, &n, &cerr)
	if rc != 0 {
		msg := "ONNX Runtime inference failed"
		if cerr != nil {
			msg = C.GoString(cerr)
			C.free(unsafe.Pointer(cerr))
		}
		return nil, fmt.Errorf("%s", msg)
	}
	defer C.free(unsafe.Pointer(out))
	bb := C.GoBytes(unsafe.Pointer(out), C.int(n*C.size_t(4)))
	vals := make([]float32, int(n))
	for i := range vals {
		vals[i] = math.Float32frombits(binary.LittleEndian.Uint32(bb[i*4 : i*4+4]))
	}
	return vals, nil
}
func (r *cgoRunner) Close() error {
	if r.ptr != nil {
		C.mgt_release(r.ptr)
		r.ptr = nil
	}
	if r.inputName != nil {
		C.free(unsafe.Pointer(r.inputName))
		r.inputName = nil
	}
	if r.outputName != nil {
		C.free(unsafe.Pointer(r.outputName))
		r.outputName = nil
	}
	return nil
}
