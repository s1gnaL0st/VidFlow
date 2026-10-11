package interfaceshttpupload

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateVideoMetadata(t *testing.T) {
	valid := &probeResult{
		Streams: []probeStream{
			{CodecType: "video", CodecName: "h264", Width: 1920, Height: 1080},
			{CodecType: "audio", CodecName: "aac"},
		},
		Format: probeFormat{Duration: "60.5"},
	}
	if err := validateVideoMetadata(valid); err != nil {
		t.Fatalf("expected valid metadata, got %v", err)
	}

	for _, codecName := range []string{"h264", "h265", "hevc", "vp8", "vp9", "av1"} {
		metadata := &probeResult{
			Streams: []probeStream{
				{CodecType: "video", CodecName: codecName, Width: 1920, Height: 1080},
				{CodecType: "audio", CodecName: "aac"},
			},
			Format: probeFormat{Duration: "60.5"},
		}
		if err := validateVideoMetadata(metadata); err != nil {
			t.Fatalf("expected %s metadata to pass, got %v", codecName, err)
		}
	}

	long := *valid
	long.Format = probeFormat{Duration: "900"}
	if err := validateVideoMetadata(&long); err == nil {
		t.Fatalf("expected long video metadata to fail")
	}

	large := *valid
	large.Streams = []probeStream{{CodecType: "video", CodecName: "h264", Width: 4096, Height: 2160}}
	if err := validateVideoMetadata(&large); err == nil {
		t.Fatalf("expected large video metadata to fail")
	}

	codec := *valid
	codec.Streams = []probeStream{{CodecType: "video", CodecName: "mpeg2video", Width: 1280, Height: 720}}
	if err := validateVideoMetadata(&codec); err == nil {
		t.Fatalf("expected unsupported codec metadata to fail")
	}
}

func TestCreateFaststartTempFileUsesMP4Extension(t *testing.T) {
	dir := t.TempDir()
	tmp, err := createFaststartTempFile(filepath.Join(dir, "clip.mp4"))
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	if filepath.Dir(tmpPath) != dir {
		t.Fatalf("expected temp file in upload dir, got %s", tmpPath)
	}
	if !strings.HasSuffix(tmpPath, ".faststart.mp4") {
		t.Fatalf("expected mp4 temp extension, got %s", tmpPath)
	}
}

func TestDefaultUploadProcessorSkipsMissingTools(t *testing.T) {
	t.Setenv("PATH", "")
	path := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(path, []byte("not-a-real-video"), 0o600); err != nil {
		t.Fatalf("write temp video: %v", err)
	}
	processor := defaultUploadProcessor{}
	if err := processor.ValidateVideo(context.Background(), path); err != nil {
		t.Fatalf("validate without ffprobe should be skipped: %v", err)
	}
	if err := processor.Faststart(context.Background(), path); err != nil {
		t.Fatalf("faststart without ffmpeg should be skipped: %v", err)
	}
}
