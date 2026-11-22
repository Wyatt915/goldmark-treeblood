package treeblood

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/wyatt915/treeblood"
)

const (
	priorityMathInlineParser = 50
	priorityMathBlockParser  = 90
	priorityMathRenderer     = 100
)

type texInlineRegionParser struct{}

func NewTexInlineRegionParser() *texInlineRegionParser {
	return &texInlineRegionParser{}
}

const (
	flavor_inline = 1 << iota
	flavor_display
	delimeter_ams
	delimeter_tex
)

var (
	_inlineopen    = []byte(`\(`)
	_inlineclose   = []byte(`\)`)
	_displayopen   = []byte(`\[`)
	_displayclose  = []byte(`\]`)
	_dollarInline  = []byte("$")
	_dollarDisplay = []byte("$$")
)

type mathInlineNode struct {
	ast.BaseInline
	flavor int
	tex    string
}

type mathBlockNode struct {
	ast.BaseBlock
	flavor int
	tex    string
	close  []byte
}

var (
	KindMathInline = ast.NewNodeKind("MathInline")
	KindMathBlock  = ast.NewNodeKind("MathBlock")
)

func (n *mathInlineNode) Kind() ast.NodeKind {
	return KindMathInline
}

func (n *mathBlockNode) Kind() ast.NodeKind {
	return KindMathBlock
}

func (n *mathInlineNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

func (n *mathBlockNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

func (p *texInlineRegionParser) Trigger() []byte {
	return []byte{'\\', '$'}
}

func (p *texInlineRegionParser) Parse(parent ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, seg := block.PeekLine()

	var begin, end []byte
	var flavor int

	switch {
	case bytes.HasPrefix(line, _inlineopen):
		begin, end = _inlineopen, _inlineclose
		flavor = flavor_inline | delimeter_ams

	case bytes.HasPrefix(line, _dollarInline):
		// Display math belongs to the block parser.
		if bytes.HasPrefix(line, _dollarDisplay) {
			return nil
		}
		begin, end = _dollarInline, _dollarInline
		flavor = flavor_inline | delimeter_tex

	default:
		// In particular, \[ is handled by the block parser.
		return nil
	}

	// Inline math must close on the same line.
	stop := bytes.Index(line[len(begin):], end)
	if stop < 0 {
		return nil
	}

	start := seg.Start + len(begin)
	stop += len(begin)

	texSeg := text.NewSegment(start, seg.Start+stop)
	tex := string(block.Value(texSeg))

	block.Advance(stop + len(end))

	return &mathInlineNode{
		tex:    tex,
		flavor: flavor,
	}
}

var mathBlockInfoKey = parser.NewContextKey()

type mathBlockData struct {
	start  int
	end    int
	flavor int
}

type texBlockRegionParser struct{}

func NewTexBlockRegionParser() *texBlockRegionParser {
	return &texBlockRegionParser{}
}

func (p *texBlockRegionParser) Trigger() []byte {
	return []byte{'$', '\\'}
}

func (p *texBlockRegionParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, seg := reader.PeekLine()

	var begin, end []byte
	var flavor int

	switch {
	case bytes.HasPrefix(line, _displayopen):
		begin, end = _displayopen, _displayclose
		flavor = flavor_display | delimeter_ams

	case bytes.HasPrefix(line, _dollarDisplay):
		begin, end = _dollarDisplay, _dollarDisplay
		flavor = flavor_display | delimeter_tex

	default:
		return nil, parser.NoChildren
	}

	// Single-line display math.
	if stop := bytes.Index(line[len(begin):], end); stop >= 0 {
		stop += len(begin)

		texSeg := text.NewSegment(
			seg.Start+len(begin),
			seg.Start+stop,
		)

		node := &mathBlockNode{
			flavor: flavor,
			tex:    string(reader.Value(texSeg)),
		}

		reader.Advance(stop + len(end))

		return node, parser.Close | parser.NoChildren
	}

	// Verify a closing delimiter exists somewhere ahead.
	posLine, posSeg := reader.Position()
	found := false

	reader.AdvanceLine()

	for {
		nextLine, _ := reader.PeekLine()
		if nextLine == nil {
			break
		}

		if bytes.Contains(nextLine, end) {
			found = true
			break
		}

		reader.AdvanceLine()
	}

	reader.SetPosition(posLine, posSeg)

	if !found {
		return nil, parser.NoChildren
	}

	reader.Advance(len(begin))

	node := &mathBlockNode{
		flavor: flavor,
		close:  end,
	}

	_, seg = reader.PeekLine()
	node.Lines().Append(seg)

	return node, parser.NoChildren
}

func (p *texBlockRegionParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	n, ok := node.(*mathBlockNode)
	if !ok {
		return parser.None
	}

	line, seg := reader.PeekLine()

	if stop := bytes.Index(line, n.close); stop >= 0 {
		node.Lines().Append(
			text.NewSegment(seg.Start, seg.Start+stop),
		)

		reader.Advance(stop + len(n.close))
		return parser.Close | parser.NoChildren
	}

	node.Lines().Append(seg)
	return parser.Continue | parser.NoChildren
}

func (p *texBlockRegionParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {
	n, ok := node.(*mathBlockNode)
	if !ok || n.tex != "" {
		return
	}

	var tex bytes.Buffer

	for i := 0; i < n.Lines().Len(); i++ {
		if i > 0 {
			tex.WriteByte('\n')
		}

		tex.Write(reader.Value(n.Lines().At(i)))
	}

	n.tex = tex.String()
}

func (p *texBlockRegionParser) CanInterruptParagraph() bool { return true }
func (p *texBlockRegionParser) CanAcceptIndentedLine() bool { return true }

func (b *texInlineRegionParser) CanInterruptParagraph() bool { return false }

func (b *texInlineRegionParser) CanAcceptIndentedLine() bool { return true }

type MathRenderer struct {
	pitz *treeblood.Pitziil
}

// NewMathRenderer returns a new MathRenderer.
func NewMathRenderer(p *treeblood.Pitziil) renderer.NodeRenderer {
	return &MathRenderer{p}
}

// RegisterFuncs registers the renderer with the Goldmark renderer.
func (r *MathRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindMathInline, r.renderMath)
	reg.Register(KindMathBlock, r.renderMath)
}

func (r *MathRenderer) renderMath(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	var tex string
	var flavor int
	switch t := node.(type) {
	case *mathInlineNode:
		flavor = t.flavor
		tex = t.tex
	case *mathBlockNode:
		flavor = t.flavor
		tex = t.tex
	default:
		return ast.WalkContinue, nil
	}
	if entering {
		var mml string
		if flavor&flavor_inline > 0 {
			mml, _ = r.pitz.TextStyle(tex)
		} else {
			mml, _ = r.pitz.DisplayStyle(tex)
		}
		w.WriteString(mml)
	}

	return ast.WalkSkipChildren, nil
}

type mathMLExtension struct {
	pitz *treeblood.Pitziil
}

func (e *mathMLExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithInlineParsers(
			util.Prioritized(NewTexInlineRegionParser(), priorityMathInlineParser),
		),
		parser.WithBlockParsers(
			util.Prioritized(NewTexBlockRegionParser(), priorityMathBlockParser),
		),
		//parser.WithASTTransformers(
		//	util.Prioritized(mathTransformer{}, priorityMathTransformer),
		//),
	)
	m.Renderer().AddOptions(
		renderer.WithNodeRenderers(
			util.Prioritized(NewMathRenderer(e.pitz), priorityMathRenderer),
		),
	)
}

// Macros to be precompiled for the document
func (e *mathMLExtension) WithMacros(macros map[string]string) *mathMLExtension {
	e.pitz.AddMacros(macros)
	return e
}

func (e *mathMLExtension) WithNumbering() *mathMLExtension {
	e.pitz.DoNumbering = true
	return e
}

func MathML(macros ...map[string]string) goldmark.Extender {
	if macros != nil {
		return &mathMLExtension{treeblood.NewDocument(macros[0], false)}
	}
	return &mathMLExtension{treeblood.NewDocument(nil, false)}
}
