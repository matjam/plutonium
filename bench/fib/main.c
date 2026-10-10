// fib: naive doubly-recursive Fibonacci. Measures call/return overhead.
#include <stdint.h>
#include <stdio.h>

#define N 43

static int64_t fib(int64_t n) {
    if (n < 2) {
        return n;
    }
    return fib(n - 1) + fib(n - 2);
}

int main(void) {
    printf("%lld\n", (long long)fib(N));
    return 0;
}
