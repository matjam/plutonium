// collatz: longest Collatz chain for starting values below N. Signed int64
// n%2 and n/2 are written as-is so the compiler has to strength-reduce them
// (signed division needs a sign fix-up unless it can prove n > 0).
// Prints the best starting value, its chain length, and the sum of all lengths.
#include <stdint.h>
#include <stdio.h>

#define N 10000000

static int64_t chainLength(int64_t n) {
    int64_t steps = 1;
    while (n != 1) {
        if (n % 2 == 0) {
            n = n / 2;
        } else {
            n = 3 * n + 1;
        }
        steps++;
    }
    return steps;
}

int main(void) {
    int64_t best = 1;
    int64_t bestLen = 1;
    int64_t total = 0;
    for (int64_t i = 1; i < N; i++) {
        int64_t l = chainLength(i);
        total += l;
        if (l > bestLen) {
            best = i;
            bestLen = l;
        }
    }
    printf("%lld\n", (long long)best);
    printf("%lld\n", (long long)bestLen);
    printf("%lld\n", (long long)total);
    return 0;
}
