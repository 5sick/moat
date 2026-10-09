#include "testing.h"

#include <cstring>

// 사용: moat_hub_tests [이름 필터]
int main(int argc, char** argv) {
    const char* filter = argc > 1 ? argv[1] : nullptr;
    int run = 0;
    for (auto& c : moat::testing::registry()) {
        if (filter && !std::strstr(c.name, filter))
            continue;
        int before = moat::testing::failures();
        c.fn();
        ++run;
        std::cout << (moat::testing::failures() == before ? "  ok   " : "  FAIL ") << c.name
                  << "\n";
    }
    std::cout << run << " tests, " << moat::testing::failures() << " failures\n";
    return moat::testing::failures() == 0 ? 0 : 1;
}
