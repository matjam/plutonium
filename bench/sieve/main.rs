// sieve: sieve of Eratosthenes over a large bool array, repeated.
// Measures byte stores with a stride, branchy scans, and memory bandwidth.

const N: usize = 20000000;
const REPS: i64 = 50;

fn sieve(flags: &mut [bool]) -> i64 {
    for i in 0..N {
        flags[i] = true;
    }
    flags[0] = false;
    flags[1] = false;
    let mut i = 2;
    while i * i < N {
        if flags[i] {
            let mut j = i * i;
            while j < N {
                flags[j] = false;
                j += i;
            }
        }
        i += 1;
    }
    let mut count: i64 = 0;
    for i in 0..N {
        if flags[i] {
            count += 1;
        }
    }
    count
}

fn main() {
    let mut flags = vec![false; N];
    let mut total: i64 = 0;
    let mut last: i64 = 0;
    for _ in 0..REPS {
        last = sieve(&mut flags);
        total += last;
    }
    println!("{}", last);
    println!("{}", total);
}
