// **Docco** is a quick-and-dirty, hundred-line-long, literate-programming-style
// documentation generator. It produces HTML
// that displays your comments alongside your code. Comments are passed through
// [Markdown](http://daringfireball.net/projects/markdown/syntax), and code is
// passed through [Pygments](http://pygments.org/) syntax highlighting.
// This page is the result of running Docco against its own source file.
//
// If you install Docco, you can run it from the command-line:
//
// docco src/*.coffee
//
// ...will generate an HTML documentation page for each of the named source files,
// with a menu linking to the other pages, saving it into a `docs` folder.
//
// The [source for Docco](http://github.com/jashkenas/docco) is available on GitHub,
// and released under the MIT license.
//
// To install Docco, first make sure you have [Node.js](http://nodejs.org/),
// [Pygments](http://pygments.org/) (install the latest dev version of Pygments
// from [its Mercurial repo](http://dev.pocoo.org/hg/pygments-main)), and
// [CoffeeScript](http://coffeescript.org/). Then, with NPM:
//
// sudo npm install -g docco
package main

import (
    "sort"
    "text/template"
    "strings"
	"bytes"
	"container/list"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sync"
	"github.com/russross/blackfriday"
)

const highlightStart = "<div class=\"highlight\"><pre>"
const highlightEnd = "</pre></div>"

type Section struct {
	docsText []byte
	codeText []byte
	DocsHTML []byte
	CodeHTML []byte
}

type TemplateSection struct {
    DocsHTML string
    CodeHTML string
    Index int
}

type Language struct {
	name           string
	symbol         string
	commentMatcher *regexp.Regexp
	dividerText    string
	dividerHTML    *regexp.Regexp
}

type TemplateData struct {
    Title string
    Sections []*TemplateSection
    Sources []string
    Multiple bool
}

var languages map[string]*Language
var sources []string

func generateDocumentation(source string, wg *sync.WaitGroup) {
	code, err := ioutil.ReadFile(source)
	if err != nil {
		log.Panic(err)
	}
	sections := parse(source, code)
	highlight(source, sections)
	generateHTML(source, sections)
	wg.Done()
}

func parse(source string, code []byte) *list.List {
	lines := bytes.Split(code, []byte("\n"))
	sections := new(list.List)
	sections.Init()
	language := getLanguage(source)

	var hasCode bool
	var codeText = new(bytes.Buffer)
	var docsText = new(bytes.Buffer)

	save := func(docs, code []byte) {
        docsCopy, codeCopy := make([]byte, len(docs)), make([]byte, len(code))
        copy(docsCopy, docs)
        copy(codeCopy, code)
		sections.PushBack(&Section{docsCopy, codeCopy, nil, nil})
	}

	for _, line := range lines {
		if language.commentMatcher.Match(line) {
			if hasCode {
				save(docsText.Bytes(), codeText.Bytes())
				hasCode = false
				codeText.Reset()
				docsText.Reset()
			}
			docsText.Write(language.commentMatcher.ReplaceAll(line, nil))
			docsText.WriteString("\n")
		} else {
			hasCode = true
			codeText.Write(line)
			codeText.WriteString("\n")
		}
	}
	save(docsText.Bytes(), codeText.Bytes())
	return sections
}

func highlight(source string, sections *list.List) {
	language := getLanguage(source)
	pygments := exec.Command("pygmentize", "-l", language.name, "-f", "html", "-O", "encoding=utf-8")
	pygmentsInput, _ := pygments.StdinPipe()
    pygmentsOutput, _ := pygments.StdoutPipe()
	pygments.Start()
	for e := sections.Front(); e != nil; e = e.Next() {
		pygmentsInput.Write(e.Value.(*Section).codeText)
		if e.Next() != nil {
			io.WriteString(pygmentsInput, language.dividerText)
		}
	}
	pygmentsInput.Close()

	buf := new(bytes.Buffer)
	io.Copy(buf, pygmentsOutput)

	output := buf.Bytes()
	output = bytes.Replace(output, []byte(highlightStart), nil, -1)
	output = bytes.Replace(output, []byte(highlightEnd), nil, -1)

	for e := sections.Front(); e != nil; e = e.Next() {
		index := language.dividerHTML.FindIndex(output)
		if index == nil {
			index = []int{len(output), len(output)}
		}

		fragment := output[0:index[0]]
		output = output[index[1]:]
		e.Value.(*Section).CodeHTML = bytes.Join([][]byte{[]byte(highlightStart), []byte(highlightEnd)}, fragment)
		e.Value.(*Section).DocsHTML = blackfriday.MarkdownCommon(e.Value.(*Section).docsText)
	}
}

// compute the output location (in `docs/`) for the file
func destination(filepath string) string {
	base := path.Base(filepath)
	return "docs/" + base[0:strings.LastIndex(base, path.Ext(base))] + ".html"
}

// render the final HTML
func generateHTML(source string, sections *list.List) {
	title := path.Base(source)
	dest := destination(source)
	// convert every `Section` into corresponding `TemplateSection`
	sectionsArray := make([]*TemplateSection, sections.Len())
	for e, i := sections.Front(), 0; e != nil; e, i = e.Next(), i+1 {
		var sec = e.Value.(*Section)
		docsBuf := bytes.NewBuffer(sec.DocsHTML)
		codeBuf := bytes.NewBuffer(sec.CodeHTML)
		sectionsArray[i] = &TemplateSection{docsBuf.String(), codeBuf.String(), i + 1}
	}
	// run through the Go template
	html := goccoTemplate(TemplateData{title, sectionsArray, sources, len(sources) > 1})
	log.Println("gocco: ", source, " -> ", dest)
	ioutil.WriteFile(dest, html, 0644)
}

func goccoTemplate(data TemplateData) []byte {
	r, x := ioutil.ReadFile("resources/gocco.got")
	if x != nil {
		panic(x)
	}
	b := new(bytes.Buffer)
	b.Write(r)

	t, err := template.New("gocco").Funcs(
		template.FuncMap{
			"base":        path.Base,
			"destination": destination,
		}).Parse(b.String())
	if err != nil {
		panic(err)
	}
	buf := new(bytes.Buffer)
	err = t.Execute(buf, data)
	if err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func getLanguage(source string) *Language {
	return languages[path.Ext(source)]
}

func ensureDirectory(name string) {
	os.MkdirAll(name, 0755)
}

func setupLanguages() {
	languages = make(map[string]*Language)
	languages[".go"] = &Language{"go", "//", nil, "", nil}
}

func setup() {
    setupLanguages()
	for _, lang := range languages {
		lang.commentMatcher, _ = regexp.Compile("^\\s*" + lang.symbol + "\\s?")
		lang.dividerText = "\n" + lang.symbol + "DIVIDER\n"
		lang.dividerHTML, _ = regexp.Compile("\\n*<span class=\"c1?\">" + lang.symbol + "DIVIDER<\\/span>\\n*")
	}
}
func main() {
	setup()

	flag.Parse()

    sources = flag.Args()
    sort.Strings(sources)

	if flag.NArg() <= 0 {
		return
	}

	ensureDirectory("docs")
	styles, _ := ioutil.ReadFile("resources/docco.css")
	ioutil.WriteFile("docs/docco.css", styles, 0755)

	wg := new(sync.WaitGroup)
	wg.Add(flag.NArg())
	for _, arg := range flag.Args() {
		go generateDocumentation(arg, wg)
	}
	wg.Wait()
}
