// heapsort: in-place heapsort of N pseudo-random int64 values from a 64-bit
// LCG. Measures data-dependent branches, random memory access, and uint64
// wrap-around multiply. Prints a sortedness flag and two hashes of the result.

const N: usize = 5000000;

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

fn sift_down(data: &mut [i64], start: usize, end: usize) {
    let mut root = start;
    loop {
        let child = 2 * root + 1;
        if child > end {
            return;
        }
        let mut swap = root;
        if data[swap] < data[child] {
            swap = child;
        }
        if child + 1 <= end && data[swap] < data[child + 1] {
            swap = child + 1;
        }
        if swap == root {
            return;
        }
        data.swap(root, swap);
        root = swap;
    }
}

fn heapsort(data: &mut [i64]) {
    let n = data.len();
    for start in (0..=(n - 2) / 2).rev() {
        sift_down(data, start, n - 1);
    }
    for end in (1..n).rev() {
        data.swap(0, end);
        sift_down(data, 0, end - 1);
    }
}

fn main() {
    let mut rng = Lcg { seed: 42 };
    let mut data = vec![0i64; N];
    for i in 0..N {
        data[i] = rng.next();
    }
    heapsort(&mut data);
    let mut sorted: i64 = 1;
    let mut hash: u64 = 0;
    for i in 0..N {
        if i > 0 && data[i - 1] > data[i] {
            sorted = 0;
        }
        hash = hash.wrapping_mul(31).wrapping_add(data[i] as u64);
    }
    println!("{}", sorted);
    println!("{}", data[N / 2]);
    println!("{}", (hash % 1000000007) as i64);
}
