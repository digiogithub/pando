package systemone

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
)

func choiceReq(model string) Request {
	return Request{
		Model: model,
		State: "I was charged twice",
		Questions: map[string]Question{
			"intent": {
				Type:         "choice",
				Instructions: "What does the user want?",
				Criteria: []Criterion{
					NewCriterion("duplicate_charge", "charged twice"),
					NewCriterion("refund_request", "wants a refund"),
					NewCriterion("other", ""),
				},
			},
		},
	}
}

func TestClientChoiceDecoding(t *testing.T) {
	s := systemonetest.NewOllama035(t)
	s.SetRawResponse(systemonetest.DuplicateChargeResponseJSON)
	c := NewClient(Options{BaseURL: s.URL})
	resp, err := c.Decide(context.Background(), choiceReq("tev1:0.8b"))
	if err != nil {
		t.Fatal(err)
	}
	a := resp.Answers["intent"]
	if a.Choice != "duplicate_charge" || a.Type != "choice" {
		t.Fatalf("answer: %+v", a)
	}
	if a.Probabilities["duplicate_charge"] != 0.9760968387170561 {
		t.Fatalf("precision lost: %v", a.Probabilities["duplicate_charge"])
	}
	if a.Confidence != 0.9145015492062412 {
		t.Fatalf("confidence: %v", a.Confidence)
	}
	if resp.Usage.InputTokens != 111 || resp.Usage.Cost != nil {
		t.Fatalf("usage: %+v", resp.Usage)
	}

	// keep_alive omitted unless set, criteria order and null descriptions preserved
	body := string(s.Requests()[0].Body)
	if strings.Contains(body, "keep_alive") {
		t.Fatalf("keep_alive must be omitted: %s", body)
	}
	if i, j, k := strings.Index(body, "duplicate_charge"), strings.Index(body, "refund_request"), strings.Index(body, `"other":null`); !(i < j && j < k) {
		t.Fatalf("criteria order not preserved: %s", body)
	}
	req := choiceReq("tev1:0.8b")
	req.KeepAlive = "30m"
	if _, err := c.Decide(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(s.Requests()[1].Body), `"keep_alive":"30m"`) {
		t.Fatal("keep_alive not sent")
	}

	// noul/score answers decode without error
	s.SetRawResponse(`{"model":"m","answers":{"n":{"type":"noul","choice":"","probabilities":{},"confidence":0},"sc":{"type":"score","score":0.5,"confidence":0.1}},"usage":{}}`)
	s2 := systemonetest.NewRemote(t)
	s2.SetRawResponse(`{"model":"m","answers":{"n":{"type":"noul","choice":"","probabilities":{},"confidence":0},"sc":{"type":"score","score":0.5,"confidence":0.1}},"usage":{}}`)
	c2 := NewClient(Options{BaseURL: s2.URL})
	r, err := c2.Decide(context.Background(), Request{Model: "jev-latest", State: "x", Questions: map[string]Question{
		"n": {Type: "noul", Instructions: "x"}, "sc": {Type: "score", Instructions: "x"},
	}})
	if err != nil || r.Answers["sc"].Score == nil || *r.Answers["sc"].Score != 0.5 {
		t.Fatalf("noul/score: %v %+v", err, r)
	}
}

func TestClientAuthHeaders(t *testing.T) {
	s := systemonetest.NewRemote(t, systemonetest.WithAPIKey("k1"), systemonetest.WithAcceptedModels("jev-latest"))
	c := NewClient(Options{BaseURL: s.URL, APIKey: "k1", Headers: map[string]string{"X-Title": "pando", "HTTP-Referer": "https://x"}})
	if _, err := c.Decide(context.Background(), choiceReq("jev-latest")); err != nil {
		t.Fatal(err)
	}
	h := s.Requests()[0].Header
	if h.Get("Authorization") != "Bearer k1" || h.Get("X-Title") != "pando" || h.Get("HTTP-Referer") != "https://x" {
		t.Fatalf("headers: %v", h)
	}

	o := systemonetest.NewOllama035(t)
	c = NewClient(Options{BaseURL: o.URL})
	if _, err := c.Decide(context.Background(), choiceReq("tev1:0.8b")); err != nil {
		t.Fatal(err)
	}
	if _, ok := o.Requests()[0].Header["Authorization"]; ok {
		t.Fatal("no key must mean no Authorization header")
	}
}

func TestClientUsageCost(t *testing.T) {
	s := systemonetest.NewRemote(t, systemonetest.WithAcceptedModels("jev-latest"))
	s.SetCost(0.0000042)
	c := NewClient(Options{BaseURL: s.URL})
	resp, err := c.Decide(context.Background(), choiceReq("jev-latest"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Usage.Cost == nil || *resp.Usage.Cost != 0.0000042 {
		t.Fatalf("cost: %+v", resp.Usage)
	}
}

func TestClientTimeout(t *testing.T) {
	s := systemonetest.NewOllama035(t)
	s.Delay(2 * time.Second)
	c := NewClient(Options{BaseURL: s.URL, Timeout: 300 * time.Millisecond})
	start := time.Now()
	_, err := c.Decide(context.Background(), choiceReq("tev1:0.8b"))
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	if d := time.Since(start); d > 400*time.Millisecond {
		t.Fatalf("took %v", d)
	}
}

func TestClientErrorMapping(t *testing.T) {
	s := systemonetest.NewOllama035(t)
	c := NewClient(Options{BaseURL: s.URL, APIKey: "sekret-key"})
	cases := []struct {
		status int
		want   error
	}{
		{401, ErrUnauthorized}, {403, ErrUnauthorized}, {404, ErrModelNotFound}, {400, ErrBadRequest},
		{413, ErrTooLarge}, {429, ErrRateLimited}, {500, ErrServer}, {503, ErrServer},
	}
	for _, tc := range cases {
		s.FailNext(tc.status)
		_, err := c.Decide(context.Background(), choiceReq("tev1:0.8b"))
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d: want %v, got %v", tc.status, tc.want, err)
		}
		var ae *APIError
		if !errors.As(err, &ae) || ae.Status != tc.status {
			t.Errorf("status %d: bad APIError %v", tc.status, err)
		}
		if tc.status >= 500 && !errors.Is(err, ErrUnavailable) {
			t.Errorf("5xx should match ErrUnavailable")
		}
	}

	// cloud model -> 400
	if _, err := c.Decide(context.Background(), choiceReq("glm-5.1:cloud")); !errors.Is(err, ErrBadRequest) {
		t.Errorf("cloud: %v", err)
	}

	// unreachable
	dead := NewClient(Options{BaseURL: "http://127.0.0.1:1", APIKey: "sekret-key", Timeout: time.Second})
	_, err := dead.Decide(context.Background(), choiceReq("tev1:0.8b"))
	if !errors.Is(err, ErrUnreachable) || !errors.Is(err, ErrUnavailable) {
		t.Errorf("unreachable: %v", err)
	}

	// key never leaks
	s.SetRawResponse("")
	s.FailNext(500)
	_, err = c.Decide(context.Background(), choiceReq("tev1:0.8b"))
	if strings.Contains(err.Error(), "sekret-key") {
		t.Errorf("key leaked: %v", err)
	}
	if c2 := (&Client{opts: Options{APIKey: "sekret-key"}}); strings.Contains(c2.redact("boom sekret-key"), "sekret-key") {
		t.Error("redact failed")
	}
}

func TestClientTooLarge(t *testing.T) {
	s := systemonetest.NewOllama035(t)
	c := NewClient(Options{BaseURL: s.URL})
	req := choiceReq("tev1:0.8b")
	req.State = strings.Repeat("x", 70*1024)
	_, err := c.Decide(context.Background(), req)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	if n := len(s.Requests()); n != 0 {
		t.Fatalf("server received %d requests", n)
	}

	// bounds
	req = choiceReq("tev1:0.8b")
	req.Questions["intent"] = Question{Type: "choice", Instructions: "x", Criteria: []Criterion{NewCriterion("only", "")}}
	if _, err := c.Decide(context.Background(), req); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("1 criterion: %v", err)
	}
	if _, err := c.Decide(context.Background(), Request{Model: "m", State: "x"}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("0 questions: %v", err)
	}
	many := map[string]Question{}
	for i := 0; i < 65; i++ {
		many[string(rune('a'+i%26))+strings.Repeat("z", i/26)] = Question{Type: "noul", Instructions: "x"}
	}
	if _, err := c.Decide(context.Background(), Request{Model: "m", State: "x", Questions: many}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("65 questions: %v", err)
	}
	if len(s.Requests()) != 0 {
		t.Fatal("bounds must be enforced locally")
	}
}

func TestClientMalformed(t *testing.T) {
	s := systemonetest.NewOllama035(t)
	c := NewClient(Options{BaseURL: s.URL})

	s.SetDecision("foo", map[string]float64{"foo": 1})
	if _, err := c.Decide(context.Background(), choiceReq("tev1:0.8b")); !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("choice not in criteria: %v", err)
	}
	s.SetRawResponse(`{"model":"m","answers":{},"usage":{}}`)
	if _, err := c.Decide(context.Background(), choiceReq("tev1:0.8b")); !errors.Is(err, ErrMalformed) {
		t.Fatalf("missing answer: %v", err)
	}
	s.SetRawResponse(`not json`)
	if _, err := c.Decide(context.Background(), choiceReq("tev1:0.8b")); !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("bad json: %v", err)
	}
}

func TestQuestionMarshalOrder(t *testing.T) {
	q := Question{Type: "choice", Instructions: "i", Criteria: []Criterion{NewCriterion("z", "last?"), NewCriterion("a", "")}}
	b, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"type":"choice","instructions":"i","criteria":{"z":"last?","a":null}}` {
		t.Fatalf("got %s", b)
	}
}
