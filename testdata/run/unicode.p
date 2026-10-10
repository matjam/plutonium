package main

import (
	"pluto/fmt"
	"pluto/unicode"
)

// The unicode package, checked against Go's: the data of every table in
// the category, script and property sets (hashed), and the result of every
// predicate and case mapping for every rune.

var tables = [...]*unicode.RangeTable{
	unicode.ASCII_Hex_Digit,
	unicode.Adlam,
	unicode.Ahom,
	unicode.Anatolian_Hieroglyphs,
	unicode.Arabic,
	unicode.Armenian,
	unicode.Avestan,
	unicode.Balinese,
	unicode.Bamum,
	unicode.Bassa_Vah,
	unicode.Batak,
	unicode.Bengali,
	unicode.Beria_Erfe,
	unicode.Bhaiksuki,
	unicode.Bidi_Control,
	unicode.Bopomofo,
	unicode.Brahmi,
	unicode.Braille,
	unicode.Buginese,
	unicode.Buhid,
	unicode.C,
	unicode.Canadian_Aboriginal,
	unicode.Carian,
	unicode.Caucasian_Albanian,
	unicode.Cc,
	unicode.Cf,
	unicode.Chakma,
	unicode.Cham,
	unicode.Cherokee,
	unicode.Chorasmian,
	unicode.Cn,
	unicode.Co,
	unicode.Common,
	unicode.Coptic,
	unicode.Cs,
	unicode.Cuneiform,
	unicode.Cypriot,
	unicode.Cypro_Minoan,
	unicode.Cyrillic,
	unicode.Dash,
	unicode.Deprecated,
	unicode.Deseret,
	unicode.Devanagari,
	unicode.Diacritic,
	unicode.Dives_Akuru,
	unicode.Dogra,
	unicode.Duployan,
	unicode.Egyptian_Hieroglyphs,
	unicode.Elbasan,
	unicode.Elymaic,
	unicode.Ethiopic,
	unicode.Extender,
	unicode.Garay,
	unicode.Georgian,
	unicode.Glagolitic,
	unicode.Gothic,
	unicode.Grantha,
	unicode.Greek,
	unicode.Gujarati,
	unicode.Gunjala_Gondi,
	unicode.Gurmukhi,
	unicode.Gurung_Khema,
	unicode.Han,
	unicode.Hangul,
	unicode.Hanifi_Rohingya,
	unicode.Hanunoo,
	unicode.Hatran,
	unicode.Hebrew,
	unicode.Hex_Digit,
	unicode.Hiragana,
	unicode.Hyphen,
	unicode.IDS_Binary_Operator,
	unicode.IDS_Trinary_Operator,
	unicode.IDS_Unary_Operator,
	unicode.ID_Compat_Math_Continue,
	unicode.ID_Compat_Math_Start,
	unicode.Ideographic,
	unicode.Imperial_Aramaic,
	unicode.Inherited,
	unicode.Inscriptional_Pahlavi,
	unicode.Inscriptional_Parthian,
	unicode.Javanese,
	unicode.Join_Control,
	unicode.Kaithi,
	unicode.Kannada,
	unicode.Katakana,
	unicode.Kawi,
	unicode.Kayah_Li,
	unicode.Kharoshthi,
	unicode.Khitan_Small_Script,
	unicode.Khmer,
	unicode.Khojki,
	unicode.Khudawadi,
	unicode.Kirat_Rai,
	unicode.L,
	unicode.LC,
	unicode.Lao,
	unicode.Latin,
	unicode.Lepcha,
	unicode.Limbu,
	unicode.Linear_A,
	unicode.Linear_B,
	unicode.Lisu,
	unicode.Ll,
	unicode.Lm,
	unicode.Lo,
	unicode.Logical_Order_Exception,
	unicode.Lt,
	unicode.Lu,
	unicode.Lycian,
	unicode.Lydian,
	unicode.M,
	unicode.Mahajani,
	unicode.Makasar,
	unicode.Malayalam,
	unicode.Mandaic,
	unicode.Manichaean,
	unicode.Marchen,
	unicode.Masaram_Gondi,
	unicode.Mc,
	unicode.Me,
	unicode.Medefaidrin,
	unicode.Meetei_Mayek,
	unicode.Mende_Kikakui,
	unicode.Meroitic_Cursive,
	unicode.Meroitic_Hieroglyphs,
	unicode.Miao,
	unicode.Mn,
	unicode.Modi,
	unicode.Modifier_Combining_Mark,
	unicode.Mongolian,
	unicode.Mro,
	unicode.Multani,
	unicode.Myanmar,
	unicode.N,
	unicode.Nabataean,
	unicode.Nag_Mundari,
	unicode.Nandinagari,
	unicode.Nd,
	unicode.New_Tai_Lue,
	unicode.Newa,
	unicode.Nko,
	unicode.Nl,
	unicode.No,
	unicode.Noncharacter_Code_Point,
	unicode.Nushu,
	unicode.Nyiakeng_Puachue_Hmong,
	unicode.Ogham,
	unicode.Ol_Chiki,
	unicode.Ol_Onal,
	unicode.Old_Hungarian,
	unicode.Old_Italic,
	unicode.Old_North_Arabian,
	unicode.Old_Permic,
	unicode.Old_Persian,
	unicode.Old_Sogdian,
	unicode.Old_South_Arabian,
	unicode.Old_Turkic,
	unicode.Old_Uyghur,
	unicode.Oriya,
	unicode.Osage,
	unicode.Osmanya,
	unicode.Other_Alphabetic,
	unicode.Other_Default_Ignorable_Code_Point,
	unicode.Other_Grapheme_Extend,
	unicode.Other_ID_Continue,
	unicode.Other_ID_Start,
	unicode.Other_Lowercase,
	unicode.Other_Math,
	unicode.Other_Uppercase,
	unicode.P,
	unicode.Pahawh_Hmong,
	unicode.Palmyrene,
	unicode.Pattern_Syntax,
	unicode.Pattern_White_Space,
	unicode.Pau_Cin_Hau,
	unicode.Pc,
	unicode.Pd,
	unicode.Pe,
	unicode.Pf,
	unicode.Phags_Pa,
	unicode.Phoenician,
	unicode.Pi,
	unicode.Po,
	unicode.Prepended_Concatenation_Mark,
	unicode.Ps,
	unicode.Psalter_Pahlavi,
	unicode.Quotation_Mark,
	unicode.Radical,
	unicode.Regional_Indicator,
	unicode.Rejang,
	unicode.Runic,
	unicode.S,
	unicode.STerm,
	unicode.Samaritan,
	unicode.Saurashtra,
	unicode.Sc,
	unicode.Sentence_Terminal,
	unicode.Sharada,
	unicode.Shavian,
	unicode.Siddham,
	unicode.Sidetic,
	unicode.SignWriting,
	unicode.Sinhala,
	unicode.Sk,
	unicode.Sm,
	unicode.So,
	unicode.Soft_Dotted,
	unicode.Sogdian,
	unicode.Sora_Sompeng,
	unicode.Soyombo,
	unicode.Sundanese,
	unicode.Sunuwar,
	unicode.Syloti_Nagri,
	unicode.Syriac,
	unicode.Tagalog,
	unicode.Tagbanwa,
	unicode.Tai_Le,
	unicode.Tai_Tham,
	unicode.Tai_Viet,
	unicode.Tai_Yo,
	unicode.Takri,
	unicode.Tamil,
	unicode.Tangsa,
	unicode.Tangut,
	unicode.Telugu,
	unicode.Terminal_Punctuation,
	unicode.Thaana,
	unicode.Thai,
	unicode.Tibetan,
	unicode.Tifinagh,
	unicode.Tirhuta,
	unicode.Todhri,
	unicode.Tolong_Siki,
	unicode.Toto,
	unicode.Tulu_Tigalari,
	unicode.Ugaritic,
	unicode.Unified_Ideograph,
	unicode.Vai,
	unicode.Variation_Selector,
	unicode.Vithkuqi,
	unicode.Wancho,
	unicode.Warang_Citi,
	unicode.White_Space,
	unicode.Yezidi,
	unicode.Yi,
	unicode.Z,
	unicode.Zanabazar_Square,
	unicode.Zl,
	unicode.Zp,
	unicode.Zs,
}

func mix(h, x uint64) uint64 {
	h ^= x
	h *= 1099511628211
	return h
}

func tableHash(t *unicode.RangeTable) uint64 {
	h := uint64(14695981039346656037)
	h = mix(h, uint64(t.LatinOffset))
	h = mix(h, uint64(len(t.R16)))
	for _, r := range t.R16 {
		h = mix(h, uint64(r.Lo))
		h = mix(h, uint64(r.Hi))
		h = mix(h, uint64(r.Stride))
	}
	h = mix(h, uint64(len(t.R32)))
	for _, r := range t.R32 {
		h = mix(h, uint64(r.Lo))
		h = mix(h, uint64(r.Hi))
		h = mix(h, uint64(r.Stride))
	}
	return h
}

type counter struct {
	name string
	n    int
	h    uint64
}

func (c *counter) add(r rune, ok bool) {
	if ok {
		c.n++
		c.h = mix(c.h, uint64(r))
	}
}

func main() {
	var th uint64
	for i, t := range tables {
		th = mix(th, tableHash(t))
		th = mix(th, uint64(i))
	}
	fmt.Println("tables", len(tables), th)

	var (
		letter, digit, space, upper, lower, title, punct, prnt, graphic counter
		control, mark, number, symbol, inUpper, inHan, inLatin          counter
		toUpper, toLower, toTitle, fold                                 counter
	)
	counters := []*counter{
		&letter, &digit, &space, &upper, &lower, &title, &punct, &prnt, &graphic,
		&control, &mark, &number, &symbol, &inUpper, &inHan, &inLatin,
		&toUpper, &toLower, &toTitle, &fold,
	}
	names := []string{
		"IsLetter", "IsDigit", "IsSpace", "IsUpper", "IsLower", "IsTitle", "IsPunct", "IsPrint", "IsGraphic",
		"IsControl", "IsMark", "IsNumber", "IsSymbol", "Is(Upper)", "Is(Han)", "In(Latin, Greek)",
		"ToUpper", "ToLower", "ToTitle", "SimpleFold",
	}
	for i, c := range counters {
		c.name = names[i]
		c.h = 14695981039346656037
	}
	// Runes beyond MaxRune and negative runes are included.
	for r := rune(-3); r <= unicode.MaxRune+3; r++ {
		letter.add(r, unicode.IsLetter(r))
		digit.add(r, unicode.IsDigit(r))
		space.add(r, unicode.IsSpace(r))
		upper.add(r, unicode.IsUpper(r))
		lower.add(r, unicode.IsLower(r))
		title.add(r, unicode.IsTitle(r))
		punct.add(r, unicode.IsPunct(r))
		prnt.add(r, unicode.IsPrint(r))
		graphic.add(r, unicode.IsGraphic(r))
		control.add(r, unicode.IsControl(r))
		mark.add(r, unicode.IsMark(r))
		number.add(r, unicode.IsNumber(r))
		symbol.add(r, unicode.IsSymbol(r))
		inUpper.add(r, unicode.Is(unicode.Upper, r))
		inHan.add(r, unicode.Is(unicode.Han, r))
		inLatin.add(r, unicode.In(r, unicode.Latin, unicode.Greek))
		toUpper.add(r, unicode.ToUpper(r) != r)
		toUpper.h = mix(toUpper.h, uint64(unicode.ToUpper(r)))
		toLower.add(r, unicode.ToLower(r) != r)
		toLower.h = mix(toLower.h, uint64(unicode.ToLower(r)))
		toTitle.add(r, unicode.ToTitle(r) != r)
		toTitle.h = mix(toTitle.h, uint64(unicode.ToTitle(r)))
		fold.add(r, unicode.SimpleFold(r) != r)
		fold.h = mix(fold.h, uint64(unicode.SimpleFold(r)))
		if unicode.To(unicode.UpperCase, r) != unicode.ToUpper(r) ||
			unicode.To(unicode.LowerCase, r) != unicode.ToLower(r) ||
			unicode.To(unicode.TitleCase, r) != unicode.ToTitle(r) {
			fmt.Println("To disagrees with ToUpper, ToLower or ToTitle at", r)
		}
	}
	for _, c := range counters {
		fmt.Println(c.name, c.n, c.h)
	}

	// The Turkish and Azeri special cases.
	for _, r := range [...]rune{'i', 'I', 'ı', 'İ', 'a', 'Z'} {
		fmt.Println(r, unicode.TurkishCase.ToUpper(r), unicode.TurkishCase.ToLower(r), unicode.TurkishCase.ToTitle(r),
			unicode.AzeriCase.ToUpper(r), unicode.AzeriCase.ToLower(r))
	}

	// SimpleFold orbits.
	for _, r := range [...]rune{'A', 'k', 'K', 'K', 's', 'S', 'ſ', 'µ', 'Μ', 'μ', 'ǅ', 'ß', 'ẞ', '1'} {
		fmt.Printf("%d:", r)
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			fmt.Printf(" %d", f)
		}
		fmt.Printf("\n")
	}
}
