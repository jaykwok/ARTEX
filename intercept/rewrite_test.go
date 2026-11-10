package intercept

import (
	"encoding/json"
	"testing"
)

func TestReviewEditContractAndNarrowing(t *testing.T) {
	before := json.RawMessage(`{"command":"fixture","timeout":30,"threads":8}`)
	if !NarrowingEdit(before, []byte(`{"command":"fixture","timeout":15,"threads":2}`)) {
		t.Fatal("safe reduction rejected")
	}
	for _, s := range []string{`{"command":"new","timeout":15,"threads":2}`, `{"command":"fixture","timeout":0,"threads":2}`, `{"command":"fixture","timeout":31,"threads":2}`, `{"timeout":15}`} {
		if NarrowingEdit(before, []byte(s)) {
			t.Fatal("unsafe automatic rewrite", s)
		}
	}
	valid := `{"decision":"allow","comment":"实际操作：收窄并发；成功后的后果：降低负载；命中规则：A1","rule_id":"A1","edited_arguments":{"threads":2}}`
	v := ParseVerdict(valid)
	if v.Action != "allow" || v.RuleCode != "A1" || len(v.EditedArguments) == 0 {
		t.Fatal(v)
	}
	for _, s := range []string{valid[:len(valid)-1], `{"decision":"allow","comment":"实际操作：收窄；成功后的后果：降负载；命中规则：A1","edited_arguments":{}}`} {
		if ParseVerdict(s).Action != "" {
			t.Fatal("incomplete edit accepted")
		}
	}
}
