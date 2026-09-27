package main

import (
	"flag"
	"log"
	"net"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	"mosgortrans-ml-go/internal/core"
	"mosgortrans-ml-go/internal/grpcapi"
	"mosgortrans-ml-go/internal/offline"
	"mosgortrans-ml-go/internal/ort"
	pb "mosgortrans-ml-go/proto"
)

func main() {
	bind := flag.String("bind", "0.0.0.0:50051", "gRPC listen address")
	modelDir := flag.String("model-dir", "./models", "directory containing ONNX files + model_manifest.json")
	generateSubmission := flag.Bool("generate-submission", false, "generate validate submission.csv before starting gRPC (default: false)")
	submissionOnly := flag.Bool("submission-only", false, "exit after generating submission; requires --generate-submission")
	datasetDir := flag.String("dataset-dir", "../dataset", "dataset root containing validate/ and sample_submission.csv")
	submissionOut := flag.String("submission-out", "./submission.csv", "output path for generated submission")
	flag.Parse()

	if *submissionOnly && !*generateSubmission {
		log.Fatal("--submission-only requires --generate-submission")
	}

	manifest, err := core.LoadManifest(*modelDir)
	if err != nil {
		log.Fatal(err)
	}
	if err = core.VerifyModelFiles(*modelDir, manifest); err != nil {
		log.Fatal(err)
	}

	reg, err := ort.New(filepath.Join(*modelDir, manifest.Models.Residual.File), manifest.Models.Residual.InputName, manifest.Models.Residual.OutputName)
	if err != nil {
		log.Fatal(err)
	}
	risk, err := ort.New(filepath.Join(*modelDir, manifest.Models.Risk.File), manifest.Models.Risk.InputName, manifest.Models.Risk.OutputName)
	if err != nil {
		_ = reg.Close()
		log.Fatal(err)
	}

	if *generateSubmission {
		offlineSvc := core.NewService(reg, risk, manifest.Models.Risk.PositiveClassIndex)
		result, err := offline.GenerateSubmission(offlineSvc, *datasetDir, *submissionOut)
		if err != nil {
			_ = reg.Close()
			_ = risk.Close()
			log.Fatalf("submission generation failed: %v", err)
		}
		log.Printf("submission generated: %s rows=%d", result.OutputPath, result.Rows)
		if *submissionOnly {
			_ = reg.Close()
			_ = risk.Close()
			return
		}
	}

	// Use a clean state for live serving. The ONNX runners are reused, but
	// telemetry/cur_dev histories from offline validation never leak into gRPC.
	svc := core.NewService(reg, risk, manifest.Models.Risk.PositiveClassIndex)
	defer svc.Close()

	lis, err := net.Listen("tcp", *bind)
	if err != nil {
		log.Fatal(err)
	}

	server := grpc.NewServer(
		grpc.MaxRecvMsgSize(4<<20),
		grpc.MaxSendMsgSize(4<<20),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	pb.RegisterMLInferenceServer(server, grpcapi.New(svc))
	reflection.Register(server)

	log.Printf("Go ONNX ML gRPC service ready on %s model=%s features=%d service=mosgortrans.v1.MLInference", *bind, core.ModelVersion, len(core.ProductionFeatures))
	if err := server.Serve(lis); err != nil {
		log.Fatal(err)
	}
}
