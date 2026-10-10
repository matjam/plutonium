// fannkuch: Benchmarks Game fannkuch-redux (sequential permutation walk).
// Measures small fixed-size array shuffling and tight integer loops.
// Prints the checksum and the maximum number of flips.

const N: usize = 11;

fn fannkuch(n: usize, max_flips_out: &mut i64) -> i64 {
    let mut perm = [0usize; 16];
    let mut perm1 = [0usize; 16];
    let mut count = [0usize; 16];
    for i in 0..n {
        perm1[i] = i;
    }
    let mut max_flips: i64 = 0;
    let mut checksum: i64 = 0;
    let mut perm_count: i64 = 0;
    let mut r = n;
    loop {
        while r != 1 {
            count[r - 1] = r;
            r -= 1;
        }
        for i in 0..n {
            perm[i] = perm1[i];
        }
        let mut flips: i64 = 0;
        let mut k = perm[0];
        while k != 0 {
            let mut i = 0;
            let mut j = k;
            while i < j {
                perm.swap(i, j);
                i += 1;
                j -= 1;
            }
            flips += 1;
            k = perm[0];
        }
        if flips > max_flips {
            max_flips = flips;
        }
        if perm_count % 2 == 0 {
            checksum += flips;
        } else {
            checksum -= flips;
        }
        // Generate the next permutation.
        loop {
            if r == n {
                *max_flips_out = max_flips;
                return checksum;
            }
            let perm0 = perm1[0];
            for i in 0..r {
                perm1[i] = perm1[i + 1];
            }
            perm1[r] = perm0;
            count[r] -= 1;
            if count[r] > 0 {
                break;
            }
            r += 1;
        }
        perm_count += 1;
    }
}

fn main() {
    let mut max_flips: i64 = 0;
    let checksum = fannkuch(N, &mut max_flips);
    println!("{}", checksum);
    println!("{}", max_flips);
}
