package scopesearch

// SQLite's public-domain fts5_unicode2.c remove_diacritics table, mode 1.
// Preserve unicode61's historical exception for multi-diacritic precomposed
// characters. A generic Unicode decomposition would silently change that rule.
// Source: SQLite bundled by modernc.org/sqlite v1.38.2.
const NormalizationVersion = 2

func foldDiacritic(c rune) rune {
	if c < 128 || c > 0xffff {
		return c
	}
	key := uint32(c)<<3 | 7
	lo, hi := 0, len(diacriticRanges)
	for lo < hi {
		mid := (lo + hi) / 2
		if uint32(diacriticRanges[mid]) <= key {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	i := lo - 1
	if i < 0 || diacriticChars[i]&0x80 != 0 || c > rune(diacriticRanges[i]>>3)+rune(diacriticRanges[i]&7) {
		return c
	}
	return rune(diacriticChars[i])
}

var diacriticRanges = [...]uint16{
	0, 1797, 1848, 1859, 1891, 1928, 1940, 1995, 2024, 2040, 2060, 2110, 2168, 2206,
	2264, 2286, 2344, 2383, 2472, 2488, 2516, 2596, 2668, 2732, 2782, 2842, 2894, 2954,
	2984, 3000, 3028, 3336, 3456, 3696, 3712, 3728, 3744, 3766, 3832, 3896, 3912, 3928,
	3944, 3968, 4008, 4040, 4056, 4106, 4138, 4170, 4202, 4234, 4266, 4296, 4312, 4344,
	4408, 4424, 4442, 4472, 4488, 4504, 6148, 6198, 6264, 6280, 6360, 6429, 6505, 6529,
	61448, 61468, 61512, 61534, 61592, 61610, 61642, 61672, 61688, 61704, 61726, 61784, 61800, 61816,
	61836, 61880, 61896, 61914, 61948, 61998, 62062, 62122, 62154, 62184, 62200, 62218, 62252, 62302,
	62364, 62410, 62442, 62478, 62536, 62554, 62584, 62604, 62640, 62648, 62656, 62664, 62730, 62766,
	62830, 62890, 62924, 62974, 63032, 63050, 63082, 63118, 63182, 63242, 63274, 63310, 63368, 63390,
}
var diacriticChars = [...]byte{
	0, 97, 99, 101, 105, 110, 111, 117, 121, 121, 97, 99, 100, 101,
	101, 103, 104, 105, 106, 107, 108, 110, 111, 114, 115, 116, 117, 117,
	119, 121, 122, 111, 117, 97, 105, 111, 117, 245, 225, 103, 107, 111,
	239, 106, 103, 110, 225, 97, 101, 105, 111, 114, 117, 115, 116, 104,
	97, 101, 239, 111, 239, 121, 0, 0, 0, 0, 0, 0, 0, 0,
	97, 98, 227, 100, 100, 229, 101, 229, 102, 103, 104, 104, 105, 233,
	107, 108, 236, 108, 109, 110, 239, 112, 114, 242, 114, 115, 243, 116,
	117, 245, 118, 119, 119, 120, 121, 122, 104, 116, 119, 121, 97, 225,
	225, 225, 101, 229, 229, 105, 111, 239, 239, 239, 117, 245, 245, 121,
}

// unicode61's combining-mark membership (U+0300..U+0331).
func isDiacritic(c rune) bool {
	if c < 768 || c > 817 {
		return false
	}
	if c < 800 {
		return (uint32(0x08029fdf)>>uint(c-768))&1 != 0
	}
	return (uint32(0x000361f8)>>uint(c-800))&1 != 0
}
