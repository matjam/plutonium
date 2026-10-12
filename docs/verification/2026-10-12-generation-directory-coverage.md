# Generation-directory coverage comparison

Complete native production profiles, baseline `a834206a` versus final generation-directory source. Each line reports all package statements and covered statements; no package is omitted. The baseline is `/tmp/plutonium-native-verification/cover-strongfield-final.out`; the passing final profile is `/tmp/plutonium-gap10-verification/cover-regclass-final.out`. Full covered build suite: 389.735 seconds, default ten-minute timeout and memory caps unchanged.

```text
arch/amd64: 2344/2571 (91.17075%) -> 2348/2574 (91.21989%), delta +0.04914, floor 86, OK
build: 349/397 (87.90932%) -> 350/397 (88.16121%), delta +0.25189, floor None, OK
constant: 1316/1417 (92.87227%) -> 1316/1417 (92.87227%), delta +0.00000, floor None, OK
debuginfo: 643/650 (98.92308%) -> 643/650 (98.92308%), delta +0.00000, floor None, OK
diag: 793/958 (82.77662%) -> 793/958 (82.77662%), delta +0.00000, floor None, OK
diag/gencatalog: 0/102 (0.00000%) -> 0/102 (0.00000%), delta +0.00000, floor None, OK
diff: 123/125 (98.40000%) -> 123/125 (98.40000%), delta +0.00000, floor 98, OK
directive: 140/142 (98.59155%) -> 140/142 (98.59155%), delta +0.00000, floor None, OK
escape: 3285/3469 (94.69588%) -> 3291/3469 (94.86884%), delta +0.17296, floor 90, OK
load: 287/322 (89.13043%) -> 287/322 (89.13043%), delta +0.00000, floor 71, OK
lower: 10307/10870 (94.82061%) -> 10364/10929 (94.83027%), delta +0.00966, floor 85, OK
lsp: 1368/1534 (89.17862%) -> 1371/1534 (89.37419%), delta +0.19557, floor 85, OK
moves: 1744/1872 (93.16239%) -> 1744/1872 (93.16239%), delta +0.00000, floor 80, OK
objfile: 156/164 (95.12195%) -> 156/164 (95.12195%), delta +0.00000, floor 95, OK
opt: 3992/4204 (94.95718%) -> 3977/4196 (94.78074%), delta -0.17644, floor 90, OK
pu: 153/154 (99.35065%) -> 153/154 (99.35065%), delta +0.00000, floor None, OK
race: 3874/4216 (91.88805%) -> 3874/4216 (91.88805%), delta +0.00000, floor 90, OK
scoped: 265/319 (83.07210%) -> 265/319 (83.07210%), delta +0.00000, floor None, OK
ssa: 823/933 (88.21008%) -> 823/933 (88.21008%), delta +0.00000, floor 79, OK
syntax/ast: 531/951 (55.83596%) -> 531/951 (55.83596%), delta +0.00000, floor None, OK
syntax/format: 109/119 (91.59664%) -> 109/119 (91.59664%), delta +0.00000, floor 91, OK
syntax/parser: 1641/1919 (85.51329%) -> 1641/1919 (85.51329%), delta +0.00000, floor 70, OK
syntax/printer: 1907/2014 (94.68719%) -> 1907/2014 (94.68719%), delta +0.00000, floor 94, OK
syntax/scanner: 464/550 (84.36364%) -> 464/550 (84.36364%), delta +0.00000, floor None, OK
syntax/token: 365/472 (77.33051%) -> 399/472 (84.53390%), delta +7.20339, floor None, OK
testguard: 125/135 (92.59259%) -> 125/135 (92.59259%), delta +0.00000, floor None, OK
types2: 10138/13240 (76.57100%) -> 10140/13240 (76.58610%), delta +0.01511, floor 56, OK
types2/errors: 3/3 (100.00000%) -> 3/3 (100.00000%), delta +0.00000, floor None, OK
aggregate: 47245/53822 (87.78009%) -> 47337/53876 (87.86287%), PASS
```

Aggregate coverage increases and every required floor passes. The optimizer's individual percentage decreases by 0.17644 points while staying above its 90% floor. Unchanged-source packages also have differing executed counters (build, escape, lsp, syntax/token and types2); the complete differences are retained rather than selecting a favorable rerun. These profiles measure which statements the entire test campaign executes, including newly compiled runtime and custom allocator programs, rather than isolating only the added Go files.
