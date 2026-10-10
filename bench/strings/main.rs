// strings: wc-style scan of a generated text buffer passed as a &[u8].
// Counts lines, words and bytes, tracks the longest word, counts words equal
// to a string constant, and folds an FNV-1a hash of every word into a running
// hash. One byte of the buffer changes before each pass.
// Measures byte indexing, bounds checks, compare chains, string indexing and
// struct return by value.

const N: usize = 16000000;
const REPS: usize = 35;
const PATTERN: &str = "go";

struct Lcg {
    seed: u64,
}

impl Lcg {
    fn next(&mut self) -> i64 {
        self.seed = self
            .seed
            .wrapping_mul(6364136223846793005)
            .wrapping_add(1442695040888963407);
        (self.seed >> 33) as i64
    }
}

#[derive(Clone, Copy, Default)]
struct Stats {
    lines: i64,
    words: i64,
    bytes: i64,
    longest: i64,
    matches: i64,
    hash: u32,
}

fn generate(text: &mut [u8], rng: &mut Lcg) {
    for i in 0..text.len() {
        let r = rng.next() % 64;
        if r < 10 {
            text[i] = b' ';
        } else if r == 10 {
            text[i] = b'\n';
        } else {
            text[i] = b'a' + (r % 26) as u8;
        }
    }
}

fn is_space(c: u8) -> bool {
    c == b' ' || c == b'\n' || c == b'\t' || c == b'\r'
}

fn matches(b: &[u8], start: usize, end: usize, pat: &str) -> bool {
    let pat = pat.as_bytes();
    if end - start != pat.len() {
        return false;
    }
    for i in 0..pat.len() {
        if b[start + i] != pat[i] {
            return false;
        }
    }
    true
}

fn scan(b: &[u8]) -> Stats {
    let mut s = Stats::default();
    let mut in_word = false;
    let mut start = 0;
    let mut word_hash: u32 = 0;
    let n = b.len();
    for i in 0..n {
        let c = b[i];
        if c == b'\n' {
            s.lines += 1;
        }
        if is_space(c) {
            if in_word {
                s.words += 1;
                s.hash = s.hash.wrapping_mul(31).wrapping_add(word_hash);
                if (i - start) as i64 > s.longest {
                    s.longest = (i - start) as i64;
                }
                if matches(b, start, i, PATTERN) {
                    s.matches += 1;
                }
                in_word = false;
            }
        } else {
            if !in_word {
                in_word = true;
                start = i;
                word_hash = 2166136261;
            }
            word_hash = (word_hash ^ c as u32).wrapping_mul(16777619);
        }
    }
    if in_word {
        s.words += 1;
        s.hash = s.hash.wrapping_mul(31).wrapping_add(word_hash);
        if (n - start) as i64 > s.longest {
            s.longest = (n - start) as i64;
        }
        if matches(b, start, n, PATTERN) {
            s.matches += 1;
        }
    }
    s.bytes = n as i64;
    s
}

fn main() {
    let mut rng = Lcg { seed: 42 };
    let mut text = vec![0u8; N];
    generate(&mut text, &mut rng);
    let mut s = Stats::default();
    let mut total_words: i64 = 0;
    let mut hash_xor: u32 = 0;
    for r in 0..REPS {
        text[(r * 7919) % N] = b'a' + (r % 26) as u8;
        s = scan(&text);
        total_words += s.words;
        hash_xor ^= s.hash;
    }
    println!("{}", s.lines);
    println!("{}", s.words);
    println!("{}", s.bytes);
    println!("{}", s.longest);
    println!("{}", s.matches);
    println!("{}", s.hash as i64);
    println!("{}", total_words);
    println!("{}", hash_xor as i64);
}
