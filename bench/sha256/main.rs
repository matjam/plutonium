// sha256: hand-written SHA-256 (FIPS 180-4, including padding) over an
// LCG-filled buffer, repeated. Each digest is written back into the start of
// the buffer, so every pass depends on the previous one.
// Measures uint32 rotates/shifts/adds with high register pressure.
// Prints the eight words of the final digest.

const N: usize = 1000003;
const REPS: usize = 350;

const K: [u32; 64] = [
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
];

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

/// Runs the compression function over every 64-byte block of p.
fn compress(hs: &mut [u32; 8], p: &[u8]) {
    let mut w = [0u32; 64];
    let n = p.len();
    let mut off = 0;
    while off + 64 <= n {
        for t in 0..16 {
            let j = off + t * 4;
            w[t] = (p[j] as u32) << 24 | (p[j + 1] as u32) << 16 | (p[j + 2] as u32) << 8 | p[j + 3] as u32;
        }
        for t in 16..64 {
            let s0 = w[t - 15].rotate_right(7) ^ w[t - 15].rotate_right(18) ^ (w[t - 15] >> 3);
            let s1 = w[t - 2].rotate_right(17) ^ w[t - 2].rotate_right(19) ^ (w[t - 2] >> 10);
            w[t] = w[t - 16]
                .wrapping_add(s0)
                .wrapping_add(w[t - 7])
                .wrapping_add(s1);
        }
        let mut a = hs[0];
        let mut b = hs[1];
        let mut c = hs[2];
        let mut d = hs[3];
        let mut e = hs[4];
        let mut f = hs[5];
        let mut g = hs[6];
        let mut h = hs[7];
        for t in 0..64 {
            let big_s1 = e.rotate_right(6) ^ e.rotate_right(11) ^ e.rotate_right(25);
            let ch = (e & f) ^ (!e & g);
            let t1 = h
                .wrapping_add(big_s1)
                .wrapping_add(ch)
                .wrapping_add(K[t])
                .wrapping_add(w[t]);
            let big_s0 = a.rotate_right(2) ^ a.rotate_right(13) ^ a.rotate_right(22);
            let maj = (a & b) ^ (a & c) ^ (b & c);
            let t2 = big_s0.wrapping_add(maj);
            h = g;
            g = f;
            f = e;
            e = d.wrapping_add(t1);
            d = c;
            c = b;
            b = a;
            a = t1.wrapping_add(t2);
        }
        hs[0] = hs[0].wrapping_add(a);
        hs[1] = hs[1].wrapping_add(b);
        hs[2] = hs[2].wrapping_add(c);
        hs[3] = hs[3].wrapping_add(d);
        hs[4] = hs[4].wrapping_add(e);
        hs[5] = hs[5].wrapping_add(f);
        hs[6] = hs[6].wrapping_add(g);
        hs[7] = hs[7].wrapping_add(h);
        off += 64;
    }
}

/// Hashes data, returning the digest words.
fn sha256(data: &[u8]) -> [u32; 8] {
    let mut hs: [u32; 8] = [
        0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab,
        0x5be0cd19,
    ];
    let n = data.len();
    let full = n - n % 64;
    compress(&mut hs, &data[..full]);

    let mut pad = [0u8; 128];
    let rem = n - full;
    for i in 0..rem {
        pad[i] = data[full + i];
    }
    pad[rem] = 0x80;
    let mut pad_len = 64;
    if rem >= 56 {
        pad_len = 128;
    }
    let bit_len = n as u64 * 8;
    for i in 0..8 {
        pad[pad_len - 1 - i] = (bit_len >> (8 * i)) as u8;
    }
    compress(&mut hs, &pad[..pad_len]);
    hs
}

fn main() {
    let mut rng = Lcg { seed: 42 };
    let mut buf = vec![0u8; N];
    for i in 0..N {
        buf[i] = (rng.next() >> 23) as u8;
    }
    let mut hs = [0u32; 8];
    for _ in 0..REPS {
        hs = sha256(&buf);
        for i in 0..8 {
            buf[4 * i] = (hs[i] >> 24) as u8;
            buf[4 * i + 1] = (hs[i] >> 16) as u8;
            buf[4 * i + 2] = (hs[i] >> 8) as u8;
            buf[4 * i + 3] = hs[i] as u8;
        }
    }
    for i in 0..8 {
        println!("{}", hs[i] as i64);
    }
}
