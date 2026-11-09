package model

import "testing"

func TestReferenceSpansStayOutOfWords(t *testing.T) {
	cases := map[string]string{
		"看 $inv-1。":   "$inv-1",
		"服務!svc_2 很慢": "!svc_2",
		"US$5 與 C#$x": "",
		"$abc-":       "$abc-",
		"a/$x 與 &$y":  "",
		"$":           "",
	}
	long := "$"
	for i := 0; i < 65; i++ {
		long += "a"
	}
	cases[long] = ""
	for text, want := range cases {
		got := ""
		for _, span := range ReferenceSpans(text) {
			got += text[span.Start:span.End]
		}
		if got != want {
			t.Errorf("%q: got %q, want %q", text, got, want)
		}
	}
}

func TestReferenceTextNamesKnownTags(t *testing.T) {
	refs := []Reference{{ReferenceKind: ReferenceKind{Key: "invoice", Label: "帳單", Trigger: "$"}, ID: "inv-1", Title: "10 月月費"}}
	if got := ReferenceText("請看 $inv-1 和 $inv-2", refs); got != "請看 [帳單：10 月月費] 和 $inv-2" {
		t.Fatal(got)
	}
	if got := ReferenceText("沒有標籤", nil); got != "沒有標籤" {
		t.Fatal(got)
	}
}
