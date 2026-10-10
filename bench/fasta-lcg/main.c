// fasta-lcg: the random-sequence part of Benchmarks Game fasta. An integer
// LCG drives a float64 cumulative-probability lookup for the IUB (3n chars) and
// Homo sapiens (5n chars) alphabets, wrapped at 60 columns. Instead of printing
// the sequences, every output byte (newlines included) is folded into a hash.
// Measures int64 mod, int-to-float conversion, float compares in a short
// search loop, and byte emission.
#include <stdint.h>
#include <stdio.h>

#define N 10000000

#define IM 139968
#define IA 3877
#define IC 29573
#define LINE_LENGTH 60

static int64_t last = 42;
static uint64_t hash;
static int64_t count;

static const uint8_t iubChars[15] = {'a', 'c', 'g', 't', 'B', 'D', 'H', 'K', 'M', 'N', 'R', 'S', 'V', 'W', 'Y'};
static const double iubProbs[15] = {0.27, 0.12, 0.12, 0.27, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02};
static const uint8_t homoChars[4] = {'a', 'c', 'g', 't'};
static const double homoProbs[4] = {0.3029549426680, 0.1979883004921, 0.1975473066391, 0.3015094502008};

static double iubCum[15];
static double homoCum[4];

static double random_(double max) {
    last = (last * IA + IC) % IM;
    return max * (double)last / IM;
}

static void makeCumulative(const double *probs, double *cum, int64_t n) {
    double p = 0.0;
    for (int64_t i = 0; i < n; i++) {
        p += probs[i];
        cum[i] = p;
    }
}

static void emit(uint8_t c) {
    hash = hash * 31 + (uint64_t)c;
    count++;
}

static void randomFasta(const uint8_t *chars, const double *cum, int64_t m, int64_t n) {
    while (n > 0) {
        int64_t line = LINE_LENGTH;
        if (n < line) {
            line = n;
        }
        for (int64_t i = 0; i < line; i++) {
            double r = random_(1.0);
            uint8_t c = chars[m - 1];
            for (int64_t j = 0; j < m; j++) {
                if (r < cum[j]) {
                    c = chars[j];
                    break;
                }
            }
            emit(c);
        }
        emit('\n');
        n -= line;
    }
}

int main(void) {
    makeCumulative(iubProbs, iubCum, 15);
    makeCumulative(homoProbs, homoCum, 4);
    randomFasta(iubChars, iubCum, 15, 3 * (int64_t)N);
    randomFasta(homoChars, homoCum, 4, 5 * (int64_t)N);
    printf("%lld\n", (long long)count);
    printf("%lld\n", (long long)last);
    printf("%lld\n", (long long)(int64_t)(hash % 1000000007));
    return 0;
}
