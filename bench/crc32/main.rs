// crc32: byte-at-a-time table-driven CRC-32 (IEEE, reflected polynomial
// 0xEDB88320) over an LCG-filled buffer, repeated. One byte of the buffer is
// changed before each pass so the passes cannot be merged.
// Measures uint32 shifts/xors, table lookups and slice passing.
// Prints the CRC of the final pass and an xor of all pass CRCs.

const N: usize = 16000000;
const REPS: usize = 36;

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

fn make_table() -> [u32; 256] {
    let mut table = [0u32; 256];
    for i in 0..256 {
        let mut c = i as u32;
        for _ in 0..8 {
            if c & 1 != 0 {
                c = 0xEDB88320 ^ (c >> 1);
            } else {
                c = c >> 1;
            }
        }
        table[i] = c;
    }
    table
}

fn crc32(table: &[u32; 256], b: &[u8]) -> u32 {
    let mut c: u32 = 0xFFFFFFFF;
    for i in 0..b.len() {
        c = table[((c ^ b[i] as u32) & 0xFF) as usize] ^ (c >> 8);
    }
    c ^ 0xFFFFFFFF
}

fn main() {
    let table = make_table();
    let mut rng = Lcg { seed: 42 };
    let mut buf = vec![0u8; N];
    for i in 0..N {
        buf[i] = (rng.next() >> 23) as u8;
    }
    let mut acc: u32 = 0;
    let mut last: u32 = 0;
    for r in 0..REPS {
        buf[(r * 7919) % N] = r as u8;
        last = crc32(&table, &buf);
        acc ^= last;
    }
    println!("{}", last as i64);
    println!("{}", acc as i64);
}
