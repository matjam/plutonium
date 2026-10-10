// fannkuch: Benchmarks Game fannkuch-redux (sequential permutation walk).
// Measures small fixed-size array shuffling and tight integer loops.
// Prints the checksum and the maximum number of flips.
#include <stdint.h>
#include <stdio.h>

#define N 11

static int64_t fannkuch(int64_t n, int64_t *maxFlipsOut) {
    int64_t perm[16] = {0};
    int64_t perm1[16] = {0};
    int64_t count[16] = {0};
    for (int64_t i = 0; i < n; i++) {
        perm1[i] = i;
    }
    int64_t maxFlips = 0;
    int64_t checksum = 0;
    int64_t permCount = 0;
    int64_t r = n;
    for (;;) {
        while (r != 1) {
            count[r - 1] = r;
            r--;
        }
        for (int64_t i = 0; i < n; i++) {
            perm[i] = perm1[i];
        }
        int64_t flips = 0;
        for (int64_t k = perm[0]; k != 0; k = perm[0]) {
            int64_t i = 0;
            int64_t j = k;
            while (i < j) {
                int64_t t = perm[i];
                perm[i] = perm[j];
                perm[j] = t;
                i++;
                j--;
            }
            flips++;
        }
        if (flips > maxFlips) {
            maxFlips = flips;
        }
        if (permCount % 2 == 0) {
            checksum += flips;
        } else {
            checksum -= flips;
        }
        // Generate the next permutation.
        for (;;) {
            if (r == n) {
                *maxFlipsOut = maxFlips;
                return checksum;
            }
            int64_t perm0 = perm1[0];
            for (int64_t i = 0; i < r; i++) {
                perm1[i] = perm1[i + 1];
            }
            perm1[r] = perm0;
            count[r]--;
            if (count[r] > 0) {
                break;
            }
            r++;
        }
        permCount++;
    }
}

int main(void) {
    int64_t maxFlips = 0;
    int64_t checksum = fannkuch(N, &maxFlips);
    printf("%lld\n", (long long)checksum);
    printf("%lld\n", (long long)maxFlips);
    return 0;
}
