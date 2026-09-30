package systemonetest

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func post(t *testing.T, url, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestFakeServerFixtures(t *testing.T) {
	s := NewOllama035(t)

	if code, body := get(t, s.URL+"/api/version"); code != 200 || body != OllamaVersionJSON && !strings.Contains(body, `"0.35.0"`) {
		t.Fatalf("version: %d %s", code, body)
	}
	code, body := get(t, s.URL+"/api/tags")
	if code != 200 || body != OllamaTagsJSON {
		t.Fatalf("tags mismatch: %d", code)
	}
	if code, body := post(t, s.URL+"/api/show", `{"model":"tev1:0.8b"}`); code != 200 || !strings.Contains(body, "num_ctx") || !strings.Contains(body, "2050") {
		t.Fatalf("show: %d %s", code, body)
	}

	s.SetRawResponse(DuplicateChargeResponseJSON)
	code, body = post(t, s.URL+"/v1/systemone", `{"model":"tev1:0.8b","state":"x","questions":{"intent":{"type":"choice","criteria":{"duplicate_charge":null,"other":null}}}}`)
	if code != 200 || body != DuplicateChargeResponseJSON {
		t.Fatalf("systemone: %d %s", code, body)
	}
	var resp struct {
		Answers map[string]struct {
			Probabilities map[string]float64 `json:"probabilities"`
			Confidence    float64            `json:"confidence"`
		} `json:"answers"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Answers["intent"].Probabilities["duplicate_charge"] != DuplicateChargeP || resp.Answers["intent"].Confidence != DuplicateChargeConfidence {
		t.Fatal("recorded precision lost")
	}
	if s.Count("/v1/systemone") != 1 || len(s.Requests()) != 4 {
		t.Fatalf("request log wrong: %d", len(s.Requests()))
	}

	// scripted behaviour
	s.SetRawResponse("")
	s.SetDecision("b", map[string]float64{"a": 0.1, "b": 0.9})
	_, body = post(t, s.URL+"/v1/systemone", `{"model":"tev1:0.8b","state":"x","questions":{"q":{"type":"choice","criteria":{"a":null,"b":null}}}}`)
	if !strings.Contains(body, `"choice":"b"`) {
		t.Fatalf("decision not scripted: %s", body)
	}
	s.FailNext(503)
	if code, _ := get(t, s.URL+"/api/version"); code != 503 {
		t.Fatalf("FailNext: %d", code)
	}
	if code, _ := post(t, s.URL+"/v1/systemone", `{"model":"missing","state":"x","questions":{"q":{"type":"choice","criteria":{"a":null,"b":null}}}}`); code != 404 {
		t.Fatalf("missing model: %d", code)
	}
	if code, _ := post(t, s.URL+"/v1/systemone", `{"model":"x:cloud","state":"x","questions":{}}`); code != 400 {
		t.Fatalf("cloud: %d", code)
	}

	// remote mode with auth
	r := NewRemote(t, WithAPIKey("good"))
	if code, _ := get(t, r.URL+"/v1/models"); code != 401 {
		t.Fatalf("auth not enforced: %d", code)
	}
	req, _ := http.NewRequest("GET", r.URL+"/v1/models", nil)
	req.Header.Set("Authorization", "Bearer good")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("models: %d", resp2.StatusCode)
	}
}
