package service

import (
	"errors"
	"strings"
	"testing"
)

func TestValidBotHTMLAccepts(t *testing.T) {
	for _, v := range []string{
		"",
		"просто текст",
		"строка\nвторая\tс табом",
		"<b>жирный</b> <i>курсив</i> <u>подчёркнутый</u> <s>зачёркнутый</s>",
		"<strong>a</strong><em>b</em><ins>c</ins><strike>d</strike><del>e</del>",
		`<a href="https://t.me/support">поддержка</a> и <a href='tg://resolve?domain=x'>tg</a>`,
		`<a href="https://example.com/?a=1&amp;b=2">ссылка</a>`,
		"<code>sudo ./sylph</code> и <pre>много\nстрок</pre>",
		`<pre><code class="language-bash">ls</code></pre>`,
		"<blockquote>цитата</blockquote> <blockquote expandable>длинная</blockquote>",
		`<tg-spoiler>секрет</tg-spoiler> <span class="tg-spoiler">ещё</span>`,
		"<b>жирный <i>и курсив</i></b>",
		"5 &lt; 7 &amp;&amp; 7 &gt; 5, &quot;кавычки&quot;, &#128512; &#x1F600;",
		"a > b",
	} {
		if err := validBotHTML("help_text", v, 3000); err != nil {
			t.Fatalf("%q rejected: %v", v, err)
		}
	}
}

func TestValidBotHTMLRejects(t *testing.T) {
	for _, v := range []string{
		"<b>не закрыт",
		"лишний </b>",
		"<b><i>перекрест</b></i>",
		"<script>alert(1)</script>",
		"<B>заглавные</B>",
		"<br>",
		`<a>без ссылки</a>`,
		`<a href="javascript:alert(1)">x</a>`,
		`<a href="ftp://x.example">x</a>`,
		`<a href="https://x.example" onclick="y">x</a>`,
		`<a href=https://x.example>x</a>`,
		`<a href="https://x.example"><a href="https://y.example">x</a></a>`,
		`<span>без класса</span>`,
		`<span class="other">x</span>`,
		`<b class="x">x</b>`,
		"<code><b>внутри кода</b></code>",
		"<pre><i>внутри pre</i></pre>",
		"<blockquote><blockquote>двойная</blockquote></blockquote>",
		`<code class="language-go">вне pre</code>`,
		"a < b",
		"Tom & Jerry",
		"&nbsp;",
		"&#xZZ;",
		"&#xD800;",
		"&#0;",
		"&#1;",
		"&#x110000;",
		`<a href="https://x.example:99999">порт</a>`,
		`<a href="https://localhost/x">без точки</a>`,
		"нулевой\x00байт",
		"управляющий\x1bсимвол",
		"\xff\xfe",
		strings.Repeat("я", 3001),
	} {
		err := validBotHTML("help_text", v, 3000)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != "help_text" {
			t.Fatalf("%q accepted or wrong error: %v", v, err)
		}
	}
}
