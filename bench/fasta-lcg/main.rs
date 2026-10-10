// fasta-lcg: the random-sequence part of Benchmarks Game fasta. An integer
// LCG drives a float64 cumulative-probability lookup for the IUB (3n chars) and
// Homo sapiens (5n chars) alphabets, wrapped at 60 columns. Instead of printing
// the sequences, every output byte (newlines included) is folded into a hash.
// Measures int64 mod, int-to-float conversion, float compares in a short
// search loop, and byte emission.

const N: i64 = 10000000;

const IM: i64 = 139968;
const IA: i64 = 3877;
const IC: i64 = 29573;
const LINE_LENGTH: i64 = 60;

const IUB_CHARS: [u8; 15] = [
    b'a', b'c', b'g', b't', b'B', b'D', b'H', b'K', b'M', b'N', b'R', b'S', b'V', b'W', b'Y',
];
const IUB_PROBS: [f64; 15] = [
    0.27, 0.12, 0.12, 0.27, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02,
];
const HOMO_CHARS: [u8; 4] = [b'a', b'c', b'g', b't'];
const HOMO_PROBS: [f64; 4] = [0.3029549426680, 0.1979883004921, 0.1975473066391, 0.3015094502008];

struct State {
    last: i64,
    hash: u64,
    count: i64,
}

impl State {
    fn random(&mut self, max: f64) -> f64 {
        self.last = (self.last * IA + IC) % IM;
        max * self.last as f64 / IM as f64
    }

    fn emit(&mut self, c: u8) {
        self.hash = self.hash.wrapping_mul(31).wrapping_add(c as u64);
        self.count += 1;
    }

    fn random_fasta(&mut self, chars: &[u8], cum: &[f64], mut n: i64) {
        let m = cum.len();
        while n > 0 {
            let mut line = LINE_LENGTH;
            if n < line {
                line = n;
            }
            for _ in 0..line {
                let r = self.random(1.0);
                let mut c = chars[m - 1];
                for j in 0..m {
                    if r < cum[j] {
                        c = chars[j];
                        break;
                    }
                }
                self.emit(c);
            }
            self.emit(b'\n');
            n -= line;
        }
    }
}

fn make_cumulative(probs: &[f64], cum: &mut [f64]) {
    let mut p = 0.0;
    for i in 0..probs.len() {
        p += probs[i];
        cum[i] = p;
    }
}

fn main() {
    let mut iub_cum = [0.0f64; 15];
    let mut homo_cum = [0.0f64; 4];
    make_cumulative(&IUB_PROBS, &mut iub_cum);
    make_cumulative(&HOMO_PROBS, &mut homo_cum);
    let mut st = State { last: 42, hash: 0, count: 0 };
    st.random_fasta(&IUB_CHARS, &iub_cum, 3 * N);
    st.random_fasta(&HOMO_CHARS, &homo_cum, 5 * N);
    println!("{}", st.count);
    println!("{}", st.last);
    println!("{}", (st.hash % 1000000007) as i64);
}
