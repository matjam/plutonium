// quicksort: recursive Hoare-partition quicksort (middle pivot) of N
// pseudo-random int64 values from a 64-bit LCG. Measures recursion,
// unpredictable branches, and sequential memory scans.
// Prints a sortedness flag, the median, and a hash of the sorted array.
//
// Indices are i64 (j can step below lo), converted to usize at each access.

const N: usize = 10000000;

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

fn quicksort(data: &mut [i64], lo: i64, hi: i64) {
    if lo >= hi {
        return;
    }
    let pivot = data[(lo + (hi - lo) / 2) as usize];
    let mut i = lo;
    let mut j = hi;
    while i <= j {
        while data[i as usize] < pivot {
            i += 1;
        }
        while data[j as usize] > pivot {
            j -= 1;
        }
        if i <= j {
            data.swap(i as usize, j as usize);
            i += 1;
            j -= 1;
        }
    }
    if lo < j {
        quicksort(data, lo, j);
    }
    if i < hi {
        quicksort(data, i, hi);
    }
}

fn main() {
    let mut rng = Lcg { seed: 42 };
    let mut data = vec![0i64; N];
    for i in 0..N {
        data[i] = rng.next();
    }
    quicksort(&mut data, 0, N as i64 - 1);
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
