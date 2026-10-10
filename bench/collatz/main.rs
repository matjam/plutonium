// collatz: longest Collatz chain for starting values below N. Signed int64
// n%2 and n/2 are written as-is so the compiler has to strength-reduce them
// (signed division needs a sign fix-up unless it can prove n > 0).
// Prints the best starting value, its chain length, and the sum of all lengths.

const N: i64 = 10000000;

fn chain_length(mut n: i64) -> i64 {
    let mut steps: i64 = 1;
    while n != 1 {
        if n % 2 == 0 {
            n = n / 2;
        } else {
            n = 3 * n + 1;
        }
        steps += 1;
    }
    steps
}

fn main() {
    let mut best: i64 = 1;
    let mut best_len: i64 = 1;
    let mut total: i64 = 0;
    for i in 1..N {
        let l = chain_length(i);
        total += l;
        if l > best_len {
            best = i;
            best_len = l;
        }
    }
    println!("{}", best);
    println!("{}", best_len);
    println!("{}", total);
}
