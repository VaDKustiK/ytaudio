#pragma once
#include "pocketfft_hdronly.h"
#include "rhythm.hpp"
#include <array>
#include <complex>
#include <numbers>
#include <string>

namespace ytaudio {
constexpr double pi = std::numbers::pi;
inline double cents(double frequency) {
    double midi = 69 + 12 * std::log2(frequency / 440);
    return 100 * (midi - std::round(midi));
}
inline double hz(double offset) {
    return 440 * std::exp2(offset / 1200);
}
inline double circularDistance(double a, double b) {
    return std::remainder(a - b, 100.0);
}
struct Tuning {
    std::string status = "unknown";
    double offset = 0, confidence = 0;
    std::array<double, 12> chroma{};
    size_t frames = 0;
};
inline double circularProminence(const std::array<double, 100> &histogram, int peak) {
    double left = histogram[peak], right = histogram[peak];
    for (int direction : {-1, 1}) {
        double &base = direction < 0 ? left : right;
        for (int step = 1; step < 100; ++step) {
            double value = histogram[(peak + direction * step + 100) % 100];
            if (value > histogram[peak])
                break;
            base = std::min(base, value);
        }
    }
    return histogram[peak] - std::max(left, right);
}
inline void summarizeTuning(Tuning &result, const std::array<double, 100> &histogram) {
    double mass = std::accumulate(histogram.begin(), histogram.end(), 0.0);
    int best = int(std::max_element(histogram.begin(), histogram.end()) - histogram.begin());
    // Broadband/inharmonic energy creates a diffuse floor, not another tuning.
    // Keep an absolute concentration gate before removing that floor so that
    // subtracting it cannot turn noise into a confident reference frequency.
    if (mass <= 0 || histogram[best] / mass < 0.04)
        return;
    double floor = median({histogram.begin(), histogram.end()});
    double x = 0, y = 0, localMass = 0, tonalMass = 0;
    double primaryProminence = circularProminence(histogram, best);
    bool competing = false;
    for (int i = 0; i < 100; ++i) {
        double d = circularDistance(i - 50, best - 50);
        double weight = std::max(0.0, histogram[i] - floor);
        tonalMass += weight;
        if (std::abs(d) < 8) {
            x += weight * std::cos(2 * pi * (i - 50) / 100);
            y += weight * std::sin(2 * pi * (i - 50) / 100);
            localMass += weight;
        }
        // A shoulder on the dominant cluster is not an independent reference.
        // Require a separated local maximum with substantial valley prominence.
        if (std::abs(d) > 12 && histogram[i] >= histogram[(i + 99) % 100] &&
            histogram[i] > histogram[(i + 1) % 100] &&
            circularProminence(histogram, i) > 0.4 * primaryProminence)
            competing = true;
    }
    result.confidence = tonalMass > 0 ? localMass / tonalMass : 0;
    result.offset = 100 * std::atan2(y, x) / (2 * pi);
    if (competing) {
        result.status = "mixed";
        return;
    }
    // Diffuse evidence means unknown; "mixed" requires competing clusters.
    if (result.confidence < 0.55)
        return;
    result.status = std::abs(result.offset) <= 2.5 ? "standard" : "detuned";
    if (result.status == "standard")
        result.offset = 0;
}
inline Tuning tuning(const std::vector<float> &audio) {
    constexpr size_t nfft = 16384, hop = 4096;
    const double sr = 22050;
    std::vector<float> window(nfft), in(nfft);
    for (size_t i = 0; i < nfft; ++i)
        window[i] = float(0.5 - 0.5 * std::cos(2 * pi * i / nfft));
    std::vector<std::complex<float>> out(nfft / 2 + 1);
    std::array<double, 100> histogram{};
    std::vector<double> previousFrequency;
    Tuning result;
    double totalWeight = 0;
    for (size_t start = 0; start + nfft <= audio.size(); start += hop) {
        for (size_t i = 0; i < nfft; ++i)
            in[i] = audio[start + i] * window[i];
        pocketfft::r2c({nfft}, {sizeof(float)}, {sizeof(std::complex<float>)}, 0, true, in.data(),
                       out.data(), 1.0f, 1);
        std::vector<double> magnitude(out.size());
        double maximum = 0;
        for (size_t i = 0; i < out.size(); ++i) {
            magnitude[i] = std::abs(out[i]);
            maximum = std::max(maximum, magnitude[i]);
        }
        if (maximum < 0.01)
            continue;
        std::vector<std::pair<double, double>> candidates;
        for (size_t bin = 60; bin + 1 < out.size() && bin * sr / nfft < 5000; ++bin) {
            if (magnitude[bin] < maximum * 0.04 || magnitude[bin] < magnitude[bin - 1] ||
                magnitude[bin] <= magnitude[bin + 1])
                continue;
            double l = std::log(std::max(1e-12, magnitude[bin - 1])),
                   m = std::log(std::max(1e-12, magnitude[bin])),
                   r = std::log(std::max(1e-12, magnitude[bin + 1]));
            double denominator = l - 2 * m + r;
            double delta = denominator ? std::clamp(0.5 * (l - r) / denominator, -0.5, 0.5) : 0;
            double f = (bin + delta) * sr / nfft;
            candidates.emplace_back(magnitude[bin], f);
        }
        std::sort(candidates.rbegin(), candidates.rend());
        if (candidates.size() > 40)
            candidates.resize(40);
        std::vector<double> currentFrequency;
        // Normalize each frame's contribution; neither a loud section nor one
        // instrument is allowed an unlimited vote in the tuning histogram.
        double sum = 0;
        std::vector<std::pair<double, double>> reliable;
        for (auto [amplitude, f] : candidates) {
            double midi = 69 + 12 * std::log2(f / 440);
            int note = int(std::round(midi));
            if (note < 0 || note >= 100)
                continue;
            currentFrequency.push_back(f);
            bool stable = false;
            for (double previous : previousFrequency)
                if (std::abs(1200 * std::log2(f / previous)) <= 4) {
                    stable = true;
                    break;
                }
            if (!stable)
                continue;
            double offset = cents(f);
            double weight = std::sqrt(amplitude / maximum) * std::min(1.0, f / 150.0);
            reliable.emplace_back(offset, weight);
            sum += weight;
            result.chroma[(note % 12 + 12) % 12] += weight;
        }
        previousFrequency = currentFrequency;
        if (sum <= 0)
            continue;
        ++result.frames;
        for (auto [offset, w] : reliable) {
            w /= sum;
            totalWeight += w;
            for (int i = 0; i < 100; ++i) {
                double d = circularDistance(i - 50, offset);
                histogram[i] += w * std::exp(-0.5 * d * d / 4);
            }
        }
    }
    if (result.frames < 8 || totalWeight == 0)
        return result;
    summarizeTuning(result, histogram);
    return result;
}

// Librosa 0.11 CQT (equal temperament, gamma=0, norm=1, Hann, sparsity=.01,
// scale=True, constant padding). Adapted from librosa under its ISC license.
// Keep this implementation subject to native/reference tensor parity tests.
inline std::vector<std::vector<float>> cqt(const std::vector<float> &original, double offset,
                                           const std::vector<float> &half,
                                           const std::vector<float> &quarter) {
    constexpr int bins = 105, bpo = 24, originalHop = 8820;
    const double alpha = (std::exp2(2.0 / bpo) - 1) / (std::exp2(2.0 / bpo) + 1);
    int frames = int(original.size() / originalHop) + 1;
    std::vector<std::vector<float>> result(bins, std::vector<float>(frames));
    for (int octave = 0; octave < 5; ++octave) {
        int lo = std::max(0, bins - (octave + 1) * bpo), hi = bins - octave * bpo;
        double sr = octave == 0 ? 44100 : octave == 1 ? 22050 : 11025;
        int hop = octave == 0 ? originalHop : octave == 1 ? originalHop / 2 : originalHop / 4;
        const auto &audio = octave == 0 ? original : octave == 1 ? half : quarter;
        std::vector<double> lengths(hi - lo), freqs(hi - lo);
        for (int bin = lo; bin < hi; ++bin) {
            freqs[bin - lo] = 65 * std::exp2(bin / 24.0 + offset / 1200.0);
            lengths[bin - lo] = sr / (alpha * freqs[bin - lo]);
        }
        size_t nfft = 1;
        while (nfft < lengths[0])
            nfft *= 2;
        std::vector<std::vector<std::pair<size_t, std::complex<float>>>> basis(hi - lo);
        for (int bin = lo; bin < hi; ++bin) {
            double length = lengths[bin - lo], frequency = freqs[bin - lo];
            long begin = long(std::floor(-length / 2)), end = long(std::floor(length / 2));
            size_t count = end - begin, pad = (nfft - count) / 2;
            std::vector<std::complex<float>> wave(nfft), fft(nfft);
            for (size_t i = 0; i < count; ++i) {
                double window = 0.5 - 0.5 * std::cos(2 * pi * i / count);
                double phase = (begin + long(i)) * 2 * pi * frequency / sr;
                wave[pad + i] = std::complex<float>(std::cos(phase), std::sin(phase)) *
                                float(window * 2 / count * length / nfft);
            }
            pocketfft::c2c({nfft}, {sizeof(std::complex<float>)}, {sizeof(std::complex<float>)},
                           {0}, true, wave.data(), fft.data(), 1.0f, 1);
            std::vector<double> amplitudes(nfft / 2 + 1);
            for (size_t k = 0; k < amplitudes.size(); ++k)
                amplitudes[k] = std::abs(fft[k]);
            auto sorted = amplitudes;
            std::sort(sorted.begin(), sorted.end());
            double sum = std::accumulate(sorted.begin(), sorted.end(), 0.0), running = 0,
                   threshold = 0;
            for (double a : sorted) {
                running += a;
                threshold = a;
                if (running >= 0.01 * sum)
                    break;
            }
            for (size_t k = 0; k < amplitudes.size(); ++k)
                if (amplitudes[k] >= threshold)
                    basis[bin - lo].emplace_back(k, fft[k] / float(std::sqrt(length)));
        }
        std::vector<float> in(nfft);
        std::vector<std::complex<float>> fft(nfft / 2 + 1);
        for (int frame = 0; frame < frames; ++frame) {
            for (size_t j = 0; j < nfft; ++j) {
                long index = long(frame) * hop + long(j) - long(nfft) / 2;
                in[j] = index >= 0 && index < long(audio.size()) ? audio[index] : 0;
            }
            pocketfft::r2c({nfft}, {sizeof(float)}, {sizeof(std::complex<float>)}, 0, true,
                           in.data(), fft.data(), 1.0f, 1);
            for (int bin = lo; bin < hi; ++bin) {
                std::complex<float> value = 0;
                for (auto [k, weight] : basis[bin - lo])
                    value += weight * fft[k];
                result[bin][frame] = std::log1p(std::abs(value));
            }
        }
    }
    return result;
}
inline const std::array<const char *, 24> tonics = {"Ab", "Eb", "Bb", "F",  "C", "G",  "D",  "A",
                                                    "E",  "B",  "F#", "C#", "B", "F#", "C#", "Ab",
                                                    "Eb", "Bb", "F",  "C",  "G", "D",  "A",  "E"};
inline const std::array<int, 24> tonicPitch = {8,  3, 10, 5, 0, 7,  2, 9, 4, 11, 6, 1,
                                               11, 6, 1,  8, 3, 10, 5, 0, 7, 2,  9, 4};
} // namespace ytaudio
