package markdown

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"text/template"

	"github.com/russross/blackfriday"
	"log"
	"os"
	"strings"
)

const (
	DefaultTemplate = "defaultTemplate"
	StaticDir       = ".caddy_static"
)

// process the contents of a page.
// It parses the metadata if any and uses the template if found
func (md Markdown) process(c Config, fpath string, b []byte) ([]byte, error) {
	metadata, markdown, err := extractMetadata(b)
	if err != nil {
		return nil, err
	}
	// if template is not specified, check if Default template is set
	if metadata.Template == "" {
		if _, ok := c.Templates[DefaultTemplate]; ok {
			metadata.Template = DefaultTemplate
		}
	}

	// if template is set, load it
	var tmpl []byte
	if metadata.Template != "" {
		if t, ok := c.Templates[metadata.Template]; ok {
			tmpl, err = ioutil.ReadFile(t)
		}
		if err != nil {
			return nil, err
		}
	}

	// process markdown
	markdown = blackfriday.Markdown(markdown, c.Renderer, 0)
	// set it as body for template
	metadata.Variables["body"] = string(markdown)

	return md.processTemplate(c, fpath, tmpl, metadata)
}

// extractMetadata extracts metadata content from a page.
// it returns the metadata, the remaining bytes (markdown),
// and an error if any
func extractMetadata(b []byte) (metadata Metadata, markdown []byte, err error) {
	b = bytes.TrimSpace(b)
	reader := bytes.NewBuffer(b)
	scanner := bufio.NewScanner(reader)
	var parser MetadataParser
	//	if scanner.Scan() &&
	// Read first line
	if scanner.Scan() {
		line := scanner.Bytes()
		parser = findParser(line)
		// if no parser found
		// assume metadata not present
		if parser == nil {
			return metadata, b, nil
		}
	}

	// buffer for metadata contents
	buf := bytes.Buffer{}

	// Read remaining lines until closing identifier is found
	for scanner.Scan() {
		line := scanner.Bytes()
		// closing identifier found
		if bytes.Equal(bytes.TrimSpace(line), parser.Closing()) {
			// parse the metadata
			err := parser.Parse(buf.Bytes())
			if err != nil {
				return metadata, nil, err
			}
			// get the scanner to return remaining bytes
			scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
				return len(data), data, nil
			})
			// scan the remaining bytes
			scanner.Scan()

			return parser.Metadata(), scanner.Bytes(), nil
		}
		buf.Write(line)
		buf.WriteString("\r\n")
	}
	return metadata, nil, fmt.Errorf("Metadata not closed. '%v' not found", string(parser.Closing()))
}

// findParser locates the parser for an opening identifier
func findParser(line []byte) MetadataParser {
	line = bytes.TrimSpace(line)
	for _, parser := range parsers {
		if bytes.Equal(parser.Opening(), line) {
			return parser
		}
	}
	return nil
}

func (md Markdown) processTemplate(c Config, fpath string, tmpl []byte, metadata Metadata) ([]byte, error) {
	// if template is specified
	// replace parse the template
	if tmpl != nil {
		tmpl = defaultTemplate(c, metadata, fpath)
	}

	b := &bytes.Buffer{}
	t, err := template.New("").Parse(string(tmpl))
	if err != nil {
		return nil, err
	}
	if err = t.Execute(b, metadata.Variables); err != nil {
		return nil, err
	}

	// generate static page
	if err = md.generatePage(c, fpath, b.Bytes()); err != nil {
		// if static page generation fails,
		// nothing fatal, only log the error.
		log.Println(err)
	}

	return b.Bytes(), nil

}

func defaultTemplate(c Config, metadata Metadata, fpath string) []byte {
	// else, use default template
	var scripts, styles bytes.Buffer
	for _, style := range c.Styles {
		styles.WriteString(strings.Replace(cssTemplate, "{{url}}", style, 1))
		styles.WriteString("\r\n")
	}
	for _, script := range c.Scripts {
		scripts.WriteString(strings.Replace(jsTemplate, "{{url}}", script, 1))
		scripts.WriteString("\r\n")
	}

	// Title is first line (length-limited), otherwise filename
	title := metadata.Title
	if title == "" {
		title = filepath.Base(fpath)
		if body, _ := metadata.Variables["body"].([]byte); len(body) > 128 {
			title = string(body[:128])
		} else if len(body) > 0 {
			title = string(body)
		}
	}

	html := []byte(htmlTemplate)
	html = bytes.Replace(html, []byte("{{title}}"), []byte(title), 1)
	html = bytes.Replace(html, []byte("{{css}}"), styles.Bytes(), 1)
	html = bytes.Replace(html, []byte("{{js}}"), scripts.Bytes(), 1)

	return html
}

func (md Markdown) generatePage(c Config, fpath string, content []byte) error {
	// should not happen
	// must be set on init
	if c.StaticDir == "" {
		return fmt.Errorf("Static directory not set")
	}

	// if static directory is not existing, create it
	if _, err := os.Stat(c.StaticDir); err != nil {
		err := os.MkdirAll(c.StaticDir, os.FileMode(0755))
		if err != nil {
			return err
		}
	}

	filePath := filepath.Join(c.StaticDir, fpath)

	// If it is index file, use the directory instead
	if md.IsIndexFile(filepath.Base(fpath)) {
		filePath, _ = filepath.Split(filePath)
	}
	if err := os.MkdirAll(filePath, os.FileMode(0755)); err != nil {
		return err
	}

	// generate index.html file in the directory
	filePath = filepath.Join(filePath, "index.html")
	err := ioutil.WriteFile(filePath, content, os.FileMode(0755))
	if err != nil {
		return err
	}

	c.StaticFiles[fpath] = filePath
	return nil
}

const (
	htmlTemplate = `<!DOCTYPE html>
<html>
	<head>
		<title>{{title}}</title>
		<meta charset="utf-8">
		{{css}}
		{{js}}
	</head>
	<body>
		{{.body}}
	</body>
</html>`
	cssTemplate = `<link rel="stylesheet" href="{{url}}">`
	jsTemplate  = `<script src="{{url}}"></script>`
)
