// Exercise the Windows SDK/resampler/FFT include order used by the worker.
#if __cplusplus < 202002L
#error "C++20 must be reported correctly; MSVC requires /Zc:__cplusplus"
#endif

#ifdef _WIN32
#include <Windows.h>
#if defined(min) || defined(max)
#error "Windows min/max macros must be disabled with NOMINMAX"
#endif
#endif

#include "CDSPResampler.h"
#include "pocketfft_hdronly.h"
#include <algorithm>
#include <array>
#include <limits>

static_assert(std::min(1, 2) == 1);
static_assert(std::max(1, 2) == 2);
static_assert(std::numeric_limits<int>::max() > 0);

int main() {
    r8b::CDSPResampler24 resampler(44100, 22050, 4096);
    std::array<double, 4096> input{};
    double *output = nullptr;
    int count = resampler.process(input.data(), int(input.size()), output);
    return count < 0 || count > int(input.size());
}
