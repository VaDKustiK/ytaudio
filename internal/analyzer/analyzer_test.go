package analyzer

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

var validResult = fmt.Sprintf(`{"schema_version":1,"analyzer_version":%q,"model_version":%q,"tempo":{"bpm":140,"alternate_bpm":70,"confidence":1,"variable":false},"key":{"tonic":"F#","scale":"minor","status":"estimated","tuning_normalized":true},"tuning":{"reference_hz":432,"offset_cents":-31.76665,"status":"detuned"},"warnings":[]}`, Version, ModelsVersion)

func TestResultSchema(t *testing.T) {
	if _, err := ParseResult([]byte(validResult)); err != nil {
		t.Fatal(err)
	}
	cases := []string{`{"schema_version":2}`, `{"schema_version":1}`, `not json`}
	var r map[string]any
	_ = json.Unmarshal([]byte(validResult), &r)
	r["tuning"].(map[string]any)["status"] = "unknown"
	bad, _ := json.Marshal(r)
	cases = append(cases, string(bad))
	r["tuning"].(map[string]any)["status"] = "standard"
	bad, _ = json.Marshal(r)
	cases = append(cases, string(bad))
	for _, text := range cases {
		if _, err := ParseResult([]byte(text)); err == nil {
			t.Errorf("accepted invalid result %s", text)
		}
	}
}
func TestThreadBudget(t *testing.T) {
	for _, cpus := range []int{1, 2, 4, 8, 64} {
		for _, mode := range []string{"low", "balanced", "maximum"} {
			n := Threads(mode, cpus)
			if n < 1 || n > cpus || n > 16 {
				t.Fatalf("%s/%d gives %d", mode, cpus, n)
			}
		}
	}
}
func TestArchiveRejectsUnsafePathsAndSymlinks(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "C:/escape", "folder\\escape", "link"} {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			z := zip.NewWriter(&b)
			header := &zip.FileHeader{Name: name}
			if name == "link" {
				header.SetMode(os.ModeSymlink | 0777)
			}
			f, _ := z.CreateHeader(header)
			_, _ = f.Write([]byte("payload"))
			_ = z.Close()
			root := t.TempDir()
			archive := filepath.Join(root, "package.zip")
			_ = os.WriteFile(archive, b.Bytes(), 0600)
			if err := extractArchive(archive, filepath.Join(root, "unpacked")); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}
func TestRegisterRejectsBadInputsAndPreservesUploadName(t *testing.T) {
	t.Setenv("YTAUDIO_CACHE_DIR", t.TempDir())
	t.Setenv("YTAUDIO_ANALYZER_DIR", "")
	m, err := New(context.Background(), "dev", "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err = m.Register("relative.wav", false); err == nil {
		t.Fatal("relative path accepted")
	}
	file := filepath.Join(t.TempDir(), "file.wav")
	_ = os.WriteFile(file, []byte("audio"), 0600)
	selected, err := m.RegisterUpload(file, "Music & звук.wav")
	if err != nil || selected.Name != "Music & звук.wav" {
		t.Fatalf("%+v %v", selected, err)
	}
	if _, err = m.Start(selected.ID, "ffmpeg", "balanced"); err == nil {
		t.Fatal("started without installed component")
	}
}
func TestManifestDetectsTampering(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "model")
	_ = os.WriteFile(path, []byte("verified"), 0600)
	hash, _ := hashFile(context.Background(), path)
	m := Manifest{Version: manifestVersion, Models: ModelsVersion, Files: map[string]string{"model": hash}}
	data, _ := json.Marshal(m)
	_ = os.WriteFile(filepath.Join(root, "manifest.json"), data, 0600)
	if err := verifyManifest(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(path, []byte("changed"), 0600)
	if err := verifyManifest(context.Background(), root); err == nil {
		t.Fatal("tampered component accepted")
	}
}

func TestComponentFailureEnablesRepair(t *testing.T) {
	t.Setenv("YTAUDIO_CACHE_DIR", t.TempDir())
	m, err := New(context.Background(), "dev", "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.state = State{ID: "job", Ready: true}
	m.finish("job", nil, ErrComponent, false)
	if m.Snapshot().Ready {
		t.Fatal("broken component must offer repair")
	}
}

func TestTimeoutIsReportedAsFailure(t *testing.T) {
	t.Setenv("YTAUDIO_CACHE_DIR", t.TempDir())
	m, err := New(context.Background(), "dev", "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.state = State{ID: "job", Ready: true}
	m.finish("job", nil, context.DeadlineExceeded, false)
	state := m.Snapshot()
	if state.Status != "failed" || state.Error == "" || !state.Ready {
		t.Fatalf("timeout should explain the failure and allow retry: %+v", state)
	}
}
func TestModelLockMatchesNativeArtifacts(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "native-analyzer", "models.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var lock map[string]string
	if json.Unmarshal(data, &lock) != nil {
		t.Fatal("invalid lock")
	}
	if lock["version"] != ModelsVersion {
		t.Fatal("model version differs from embedded component metadata")
	}
	for name, digest := range modelDigests {
		if lock[name] != digest {
			t.Fatalf("model hash mismatch for %s", name)
		}
	}
}
