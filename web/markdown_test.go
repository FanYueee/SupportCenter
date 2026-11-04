package web

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"supportcenter/internal/model"
)

func renderMessage(t *testing.T, text string, refs map[int64]model.TicketRef) string {
	t.Helper()
	var out bytes.Buffer
	if err := MessageText(text, refs, "/tickets/").Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestMessagesShowSimpleMarkdown(t *testing.T) {
	for text, want := range map[string]string{
		"**粗體** 和 *斜體* 與 ~~刪除~~":        `<p><strong>粗體</strong> 和 <em>斜體</em> 與 <s>刪除</s></p>`,
		"**粗體 *斜體* 結束**":                `<p><strong>粗體 <em>斜體</em> 結束</strong></p>`,
		"執行 `ls *.log` 看看":              `<p>執行 <code>ls *.log</code> 看看</p>`,
		"步驟：\n- 第一\n- 第二\n\n結束":         `<p>步驟：</p><ul><li>第一</li><li>第二</li></ul><p>結束</p>`,
		"3. 三\n4. 四":                    `<ol start="3"><li>三</li><li>四</li></ol>`,
		"> 引用\n> 第二行\n回覆":               `<blockquote>引用` + "\n" + `第二行</blockquote><p>回覆</p>`,
		"```\n<b>不格式化</b> **x**\n```":   `<pre><code>&lt;b&gt;不格式化&lt;/b&gt; **x**</code></pre>`,
		"## 標題":                         `<p class="message-heading">標題</p>`,
		"第一段\n第二行\n\n\n第二段":             "<p>第一段\n第二行\n\n\n第二段</p>",
		"2 * 3 * 4 和 a*b":               `<p>2 * 3 * 4 和 a*b</p>`,
		"**https://example.com/a*b*c**": `<p><strong><a href="https://example.com/a*b*c" target="_blank" rel="noopener noreferrer nofollow ugc">https://example.com/a*b*c</a></strong></p>`,
		"單獨的 ` 符號與 #12":                 "<p>單獨的 ` 符號與 #12</p>",
		"#12 不是標題":                      `<p>#12 不是標題</p>`,
	} {
		if got := renderMessage(t, text, nil); got != want {
			t.Errorf("%q\n got  %s\n want %s", text, got, want)
		}
	}
}

func TestMarkdownNeverWritesUserHTML(t *testing.T) {
	refs := map[int64]model.TicketRef{6: {ID: 6, Subject: `"><script>x</script>`}}
	for _, text := range []string{
		`**<img src=x onerror=alert(1)>**`, "- <script>alert(1)</script>", "> <iframe src=//evil>", "`</code><script>`",
		"```\n</pre><script>alert(1)</script>\n", `[點我](javascript:alert(1))`, `[銀行](https://evil.example)`, "*#6*", `## <b onclick=x>`,
	} {
		got := renderMessage(t, text, refs)
		if strings.Contains(got, "<script") || strings.Contains(got, "<img") || strings.Contains(got, "<iframe") || strings.Contains(got, "onclick=x>") ||
			strings.Contains(got, "javascript:") && strings.Contains(got, "href=\"javascript") || strings.Contains(got, `href="https://evil.example"`) && !strings.Contains(got, ">https://evil.example<") {
			t.Errorf("%q rendered unsafe HTML: %s", text, got)
		}
	}
	// A link always shows its whole address, so [text](url) cannot hide where it goes.
	if got := renderMessage(t, `[銀行](https://evil.example)`, nil); !strings.Contains(got, `>https://evil.example</a>`) || strings.Contains(got, `>銀行</a>`) {
		t.Fatalf("disguised link: %s", got)
	}
}
