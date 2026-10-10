// mandelbrot: Benchmarks Game mandelbrot (50 iterations, escape radius 2),
// packing 8 pixels per byte exactly as the PBM writer would. Instead of writing
// the bitmap, prints the number of set bits and a hash of the packed bytes.
// Measures scalar float64 loops with a data-dependent early exit.

const N: i64 = 5000;

struct Sink {
    hash: u64,
    set_bits: i64,
}

impl Sink {
    fn emit(&mut self, b: i64) {
        self.hash = self.hash.wrapping_mul(31).wrapping_add(b as u64);
        for k in 0..8 {
            self.set_bits += (b >> k) & 1;
        }
    }
}

fn main() {
    let limit: f64 = 2.0;
    let mut sink = Sink { hash: 0, set_bits: 0 };
    let w = N;
    let h = N;
    let iter = 50;
    for y in 0..h {
        let mut byte_acc: i64 = 0;
        let mut bit_num: i64 = 0;
        let ci = 2.0 * y as f64 / h as f64 - 1.0;
        for x in 0..w {
            let cr = 2.0 * x as f64 / w as f64 - 1.5;
            let mut zr = 0.0;
            let mut zi = 0.0;
            let mut tr = 0.0;
            let mut ti = 0.0;
            let mut i = 0;
            while i < iter && tr + ti <= limit * limit {
                zi = 2.0 * zr * zi + ci;
                zr = tr - ti + cr;
                tr = zr * zr;
                ti = zi * zi;
                i += 1;
            }
            byte_acc <<= 1;
            if tr + ti <= limit * limit {
                byte_acc |= 1;
            }
            bit_num += 1;
            if bit_num == 8 {
                sink.emit(byte_acc);
                byte_acc = 0;
                bit_num = 0;
            } else if x == w - 1 {
                byte_acc <<= 8 - w % 8;
                sink.emit(byte_acc);
                byte_acc = 0;
                bit_num = 0;
            }
        }
    }
    println!("{}", sink.set_bits);
    println!("{}", (sink.hash % 1000000007) as i64);
}
