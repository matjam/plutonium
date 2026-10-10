// mandelbrot: Benchmarks Game mandelbrot (50 iterations, escape radius 2),
// packing 8 pixels per byte exactly as the PBM writer would. Instead of writing
// the bitmap, prints the number of set bits and a hash of the packed bytes.
// Measures scalar float64 loops with a data-dependent early exit.
#include <stdint.h>
#include <stdio.h>

#define N 5000

static double limit = 2.0;

static uint64_t hash;
static int64_t setBits;

static void emit(int64_t b) {
    hash = hash * 31 + (uint64_t)b;
    for (int k = 0; k < 8; k++) {
        setBits += (b >> k) & 1;
    }
}

int main(void) {
    int64_t w = N;
    int64_t h = N;
    int iter = 50;
    for (int64_t y = 0; y < h; y++) {
        int64_t byteAcc = 0;
        int64_t bitNum = 0;
        double ci = 2 * (double)y / (double)h - 1.0;
        for (int64_t x = 0; x < w; x++) {
            double cr = 2 * (double)x / (double)w - 1.5;
            double zr = 0.0;
            double zi = 0.0;
            double tr = 0.0;
            double ti = 0.0;
            for (int i = 0; i < iter && tr + ti <= limit * limit; i++) {
                zi = 2 * zr * zi + ci;
                zr = tr - ti + cr;
                tr = zr * zr;
                ti = zi * zi;
            }
            byteAcc <<= 1;
            if (tr + ti <= limit * limit) {
                byteAcc |= 1;
            }
            bitNum++;
            if (bitNum == 8) {
                emit(byteAcc);
                byteAcc = 0;
                bitNum = 0;
            } else if (x == w - 1) {
                byteAcc <<= 8 - w % 8;
                emit(byteAcc);
                byteAcc = 0;
                bitNum = 0;
            }
        }
    }
    printf("%lld\n", (long long)setBits);
    printf("%lld\n", (long long)(int64_t)(hash % 1000000007));
    return 0;
}
