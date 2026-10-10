// Adapted from Go's unicode/script_test.go. Go's tests look the tables up in
// the maps Categories and Properties, which Plutonium does not have; these
// tests name the table variables instead.

package unicode

import "pluto/testing"

type tableCase struct {
	rune rune
	name string
	tab  *RangeTable
}

var inCategoryTest = [...]tableCase{
	{0x0081, "Cc", Cc},
	{0x200B, "Cf", Cf},
	{0xf0000, "Co", Co},
	{0xdb80, "Cs", Cs},
	{0x0236, "Ll", Ll},
	{0x1d9d, "Lm", Lm},
	{0x07cf, "Lo", Lo},
	{0x1f8a, "Lt", Lt},
	{0x03ff, "Lu", Lu},
	{0x0bc1, "Mc", Mc},
	{0x20df, "Me", Me},
	{0x07f0, "Mn", Mn},
	{0x1bb2, "Nd", Nd},
	{0x10147, "Nl", Nl},
	{0x2478, "No", No},
	{0xfe33, "Pc", Pc},
	{0x2011, "Pd", Pd},
	{0x301e, "Pe", Pe},
	{0x2e03, "Pf", Pf},
	{0x2e02, "Pi", Pi},
	{0x0022, "Po", Po},
	{0x2770, "Ps", Ps},
	{0x00a4, "Sc", Sc},
	{0xa711, "Sk", Sk},
	{0x25f9, "Sm", Sm},
	{0x2108, "So", So},
	{0x2028, "Zl", Zl},
	{0x2029, "Zp", Zp},
	{0x202f, "Zs", Zs},
	// Unifieds.
	{0x04aa, "L", L},
	{0x0009, "C", C},
	{0x1712, "M", M},
	{0x0031, "N", N},
	{0x00bb, "P", P},
	{0x00a2, "S", S},
	{0x00a0, "Z", Z},
	{0x0065, "LC", LC},
	// Unassigned
	{0x0378, "Cn", Cn},
	{0x0378, "C", C},
}

var inPropTest = [...]tableCase{
	{0x0046, "ASCII_Hex_Digit", ASCII_Hex_Digit},
	{0x200F, "Bidi_Control", Bidi_Control},
	{0x2212, "Dash", Dash},
	{0xE0001, "Deprecated", Deprecated},
	{0x00B7, "Diacritic", Diacritic},
	{0x30FE, "Extender", Extender},
	{0xFF46, "Hex_Digit", Hex_Digit},
	{0x2E17, "Hyphen", Hyphen},
	{0x2FFB, "IDS_Binary_Operator", IDS_Binary_Operator},
	{0x2FF3, "IDS_Trinary_Operator", IDS_Trinary_Operator},
	{0xFA6A, "Ideographic", Ideographic},
	{0x200D, "Join_Control", Join_Control},
	{0x0EC4, "Logical_Order_Exception", Logical_Order_Exception},
	{0x2FFFF, "Noncharacter_Code_Point", Noncharacter_Code_Point},
	{0x065E, "Other_Alphabetic", Other_Alphabetic},
	{0x2065, "Other_Default_Ignorable_Code_Point", Other_Default_Ignorable_Code_Point},
	{0x0BD7, "Other_Grapheme_Extend", Other_Grapheme_Extend},
	{0x0387, "Other_ID_Continue", Other_ID_Continue},
	{0x212E, "Other_ID_Start", Other_ID_Start},
	{0x2094, "Other_Lowercase", Other_Lowercase},
	{0x2040, "Other_Math", Other_Math},
	{0x216F, "Other_Uppercase", Other_Uppercase},
	{0x0027, "Pattern_Syntax", Pattern_Syntax},
	{0x0020, "Pattern_White_Space", Pattern_White_Space},
	{0x06DD, "Prepended_Concatenation_Mark", Prepended_Concatenation_Mark},
	{0x300D, "Quotation_Mark", Quotation_Mark},
	{0x2EF3, "Radical", Radical},
	{0x1f1ff, "Regional_Indicator", Regional_Indicator},
	{0x061F, "STerm", STerm}, // Deprecated alias of Sentence_Terminal
	{0x061F, "Sentence_Terminal", Sentence_Terminal},
	{0x2071, "Soft_Dotted", Soft_Dotted},
	{0x003A, "Terminal_Punctuation", Terminal_Punctuation},
	{0x9FC3, "Unified_Ideograph", Unified_Ideograph},
	{0xFE0F, "Variation_Selector", Variation_Selector},
	{0x0020, "White_Space", White_Space},
	{0x221e, "ID_Compat_Math_Start", ID_Compat_Math_Start},
	{0x06e3, "Modifier_Combining_Mark", Modifier_Combining_Mark},
	{0x2080, "ID_Compat_Math_Continue", ID_Compat_Math_Continue},
	{0x2ffe, "IDS_Unary_Operator", IDS_Unary_Operator},
}

var inScriptTest = [...]tableCase{
	{'a', "Latin", Latin},
	{0x03b1, "Greek", Greek},
	{0x0416, "Cyrillic", Cyrillic},
	{0x05d0, "Hebrew", Hebrew},
	{0x0627, "Arabic", Arabic},
	{0x0e01, "Thai", Thai},
	{0x3042, "Hiragana", Hiragana},
	{0x30a2, "Katakana", Katakana},
	{0xac00, "Hangul", Hangul},
	{0x4e16, "Han", Han},
	{'1', "Common", Common},
	{0x0300, "Inherited", Inherited},
	{0x1e900, "Adlam", Adlam},
}

func TestCategories(t *testing.T) {
	for _, test := range inCategoryTest {
		if !Is(test.tab, test.rune) {
			t.Errorf("IsCategory(%U, %s) = false, want true", test.rune, test.name)
		}
	}
}

func TestProperties(t *testing.T) {
	for _, test := range inPropTest {
		if !Is(test.tab, test.rune) {
			t.Errorf("IsProperty(%U, %s) = false, want true", test.rune, test.name)
		}
	}
}

func TestScripts(t *testing.T) {
	for _, test := range inScriptTest {
		if !Is(test.tab, test.rune) {
			t.Errorf("IsScript(%U, %s) = false, want true", test.rune, test.name)
		}
	}
	// A rune is in at most one of these scripts.
	for i, a := range inScriptTest {
		for j, b := range inScriptTest {
			if i != j && Is(b.tab, a.rune) {
				t.Errorf("%U is in %s and %s", a.rune, a.name, b.name)
			}
		}
	}
}

func TestRangeTableShape(t *testing.T) {
	// The ranges of every table are sorted and do not overlap, R16 holds
	// only runes below 1<<16 and R32 only runes from 1<<16 up.
	for _, nt := range latinOffsetTables {
		var last uint32
		for i, r := range nt.tab.R16 {
			if r.Lo > r.Hi || r.Stride == 0 || (i > 0 && uint32(r.Lo) <= last) {
				t.Errorf("%s: bad R16 range %d: %v", nt.name, i, r)
			}
			last = uint32(r.Hi)
		}
		for i, r := range nt.tab.R32 {
			if r.Lo > r.Hi || r.Stride == 0 || r.Lo < 1<<16 || (i > 0 && r.Lo <= last) {
				t.Errorf("%s: bad R32 range %d: %v", nt.name, i, r)
			}
			last = r.Hi
		}
	}
}

func TestVersion(t *testing.T) {
	if Version != "17.0.0" {
		t.Errorf("Version = %q, want 17.0.0", Version)
	}
}
