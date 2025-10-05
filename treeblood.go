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
	_inlineopen    = []byte(`\\(`)
	_inlineclose   = []byte(`\\)`)
	_displayopen   = []byte(`\\[`)
	_displayclose  = []byte(`\\]`)
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
	case bytes.HasPrefix(line, _dollarDisplay):
		begin, end = _dollarDisplay, _dollarDisplay
		flavor = flavor_display | delimeter_tex
	case bytes.HasPrefix(line, _dollarInline):
		begin, end = _dollarInline, _dollarInline
		flavor = flavor_inline | delimeter_tex
	case bytes.HasPrefix(line, _inlineopen):
		begin, end = _inlineopen, _inlineclose
		flavor = flavor_inline | delimeter_ams
	case bytes.HasPrefix(line, _displayopen):
		begin, end = _displayopen, _displayclose
		flavor = flavor_display | delimeter_ams
	default:
		return nil
	}

	// Look for closing delimiter in current line
	stop := bytes.Index(line[len(begin):], end)
	if stop < 0 {
		// Try one lookahead line for multiline safety
		posLine, posSeg := block.Position()
		block.AdvanceLine()
		nextLine, _ := block.PeekLine()
		block.SetPosition(posLine, posSeg)
		if nextLine == nil || bytes.Index(nextLine, end) < 0 {
			// No closing delimiter → not math
			return nil
		}
		// Found closing delimiter on next line
		block.AdvanceLine()
		line2, seg2 := block.PeekLine()
		stop = bytes.Index(line2, end)
		if stop < 0 {
			return nil
		}
		texSeg := text.NewSegment(seg2.Start, seg2.Start+stop)
		tex := string(block.Value(texSeg))
		block.Advance(stop + len(end))
		return &mathInlineNode{tex: tex, flavor: flavor}
	}

	// Found closing delimiter on same line
	start := seg.Start + len(begin)
	stop += len(begin) // adjust offset
	texSeg := text.NewSegment(start, seg.Start+stop)
	tex := string(block.Value(texSeg))

	// Advance past the math region
	block.Advance(stop - seg.Start + len(end))

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
	line, _ := reader.PeekLine()
	var begin, end []byte
	var flavor int

	switch {
	case bytes.HasPrefix(line, _displayopen):
		begin, end = _displayopen, _displayclose
		flavor = flavor_display | delimeter_ams
	case bytes.HasPrefix(line, _dollarDisplay):
		begin, end = _dollarDisplay, _dollarDisplay
		flavor = flavor_display | delimeter_tex
	case bytes.HasPrefix(line, _inlineopen):
		begin, end = _inlineopen, _inlineclose
		flavor = flavor_inline | delimeter_ams
	case bytes.HasPrefix(line, _dollarInline):
		begin, end = _dollarInline, _dollarInline
		flavor = flavor_inline | delimeter_tex
	default:
		return nil, parser.NoChildren
	}

	// Make sure this is not a single-line inline math
	if bytes.Contains(line[len(begin):], end) {
		// closing on same line = inline math
		return nil, parser.NoChildren
	}

	// Look ahead for closing delimiter without consuming input
	found := false
	posLine, posSeg := reader.Position()
	for {
		_, _ = reader.PeekLine()
		reader.AdvanceLine()
		nextLine, _ := reader.PeekLine()
		if nextLine == nil {
			break
		}
		if bytes.Count(nextLine, end) == 1 {
			found = true
			break
		} else {
			found = false
			break
		}
	}
	// Restore position
	reader.SetPosition(posLine, posSeg)

	if !found {
		// No closing delimiter — let default parser handle as normal text
		return nil, parser.NoChildren
	}

	// We’re safe: consume opening line
	reader.Advance(len(begin))
	pc.Set(mathBlockInfoKey, mathBlockData{flavor: flavor})
	node := &mathBlockNode{flavor: flavor}
	_, seg := reader.PeekLine()
	node.Lines().Append(seg)
	return node, parser.NoChildren
}

func (p *texBlockRegionParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	line, seg := reader.PeekLine()
	key := pc.Get(mathBlockInfoKey)
	if key == nil {
		return parser.None
	}
	d := key.(mathBlockData)

	var closeTag []byte
	switch d.flavor {
	case flavor_inline | delimeter_ams:
		closeTag = _inlineclose
	case flavor_display | delimeter_ams:
		closeTag = _displayclose
	case flavor_inline | delimeter_tex:
		closeTag = _dollarInline
	case flavor_display | delimeter_tex:
		closeTag = _dollarDisplay
	}

	if stop := bytes.Index(line, closeTag); stop >= 0 {
		node.Lines().Append(text.NewSegment(seg.Start, seg.Start+stop))
		reader.Advance(stop + len(closeTag))
		return parser.Close | parser.NoChildren
	}

	node.Lines().Append(seg)
	return parser.Continue | parser.NoChildren
}

func (p *texBlockRegionParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {
	if d, ok := pc.Get(mathBlockInfoKey).(mathBlockData); ok {
		if n, ok := node.(*mathBlockNode); ok {
			for i := 0; i < n.Lines().Len(); i++ {
				n.tex += string(reader.Value(n.Lines().At(i)))
			}
			n.flavor = d.flavor
		}
	}
	pc.Set(mathBlockInfoKey, nil)
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
