#pragma once
#include <algorithm>
#include <cmath>
#include <numeric>
#include <vector>

namespace ytaudio {
inline double median(std::vector<double> x) {
    if (x.empty())
        return 0;
    std::sort(x.begin(), x.end());
    const auto n = x.size();
    return n % 2 ? x[n / 2] : (x[n / 2 - 1] + x[n / 2]) / 2;
}
struct Tempo {
    double bpm = 0, alternate = 0, support = 0;
    bool variable = false;
};
inline Tempo tempo(const std::vector<double> &beats) {
    Tempo result;
    if (beats.size() < 8)
        return result;
    std::vector<double> intervals;
    for (size_t i = 1; i < beats.size(); ++i) {
        double d = beats[i] - beats[i - 1];
        if (d >= 0.15 && d <= 3)
            intervals.push_back(d);
    }
    if (intervals.size() < 6)
        return result;
    // Find an actual observed interval cluster, allowing skipped/extra beats.
    double best = 0;
    size_t score = 0;
    for (double candidate : intervals) {
        size_t count = 0;
        for (double d : intervals)
            if (std::abs(d / candidate - 1) < 0.07)
                ++count;
        if (count > score) {
            score = count;
            best = candidate;
        }
    }
    std::vector<double> aligned;
    for (double d : intervals) {
        if (std::abs(d / best - 1) < 0.07)
            aligned.push_back(d);
    }
    result.support = double(aligned.size()) / intervals.size();
    // Fold missed/extra beats before pooling a section. A median of equal
    // numbers of single and double intervals invents a nonexistent 1.5x beat.
    size_t different = 0, sections = 0;
    for (size_t i = 0; i + 8 <= intervals.size(); i += 8) {
        std::vector<double> section;
        for (size_t j = i; j < i + 8; ++j) {
            double s = intervals[j], ratio = s / best;
            if (ratio > 1.8 && ratio < 2.2)
                s /= 2;
            else if (ratio > 0.45 && ratio < 0.55)
                s *= 2;
            section.push_back(s);
        }
        double center = median(section);
        auto coherent = std::count_if(section.begin(), section.end(), [center](double s) {
            return std::abs(s / center - 1) < 0.07;
        });
        ++sections;
        // Scattered tracking errors are not evidence of another stable tempo.
        if (coherent >= 6 && std::abs(center / best - 1) > 0.10)
            ++different;
    }
    result.variable = sections >= 3 && different >= 2;
    if (result.support < 0.45)
        return result;
    // Averaging only the verified cluster avoids median bias from the 20 ms
    // beat-frame quantization (e.g. 140 BPM alternates 0.42/0.44 s).
    result.bpm = 60 / (std::accumulate(aligned.begin(), aligned.end(), 0.0) / aligned.size());
    // An octave-equivalent interpretation is useful, but not producer metadata.
    result.alternate = result.bpm < 100 ? result.bpm * 2 : result.bpm / 2;
    return result;
}
// Exact minimal-postprocessor semantics: pool the two channels independently.
inline std::vector<double> peaks(const std::vector<float> &logits) {
    std::vector<double> positions;
    for (size_t i = 0; i < logits.size(); ++i) {
        if (logits[i] <= 0)
            continue;
        bool maximum = true;
        for (int offset = -3; offset <= 3; ++offset) {
            auto j = static_cast<long long>(i) + offset;
            if (j >= 0 && j < static_cast<long long>(logits.size()) && logits[j] > logits[i])
                maximum = false;
        }
        if (maximum)
            positions.push_back(double(i));
    }
    std::vector<double> times;
    double center = 0;
    int count = 0;
    for (double p : positions) {
        if (count && p - center > 1) {
            // The official implementation retains sub-frame peak means.
            times.push_back(center / 50);
            count = 0;
        }
        if (!count) {
            center = p;
            count = 1;
        } else {
            ++count;
            center += (p - center) / count;
        }
    }
    if (count)
        times.push_back(center / 50);
    return times;
}
inline std::vector<double> snapDownbeats(const std::vector<double> &beats,
                                         const std::vector<double> &downbeats) {
    if (beats.empty())
        return downbeats;
    std::vector<double> result;
    for (double time : downbeats) {
        auto nearest = std::min_element(beats.begin(), beats.end(), [time](double a, double b) {
            return std::abs(a - time) < std::abs(b - time);
        });
        result.push_back(*nearest);
    }
    std::sort(result.begin(), result.end());
    result.erase(std::unique(result.begin(), result.end()), result.end());
    return result;
}
inline double downbeatSupport(const std::vector<double> &beats,
                              const std::vector<double> &downbeats) {
    if (downbeats.size() < 4 || beats.empty())
        return 1;
    std::vector<size_t> spans;
    size_t previous = 0;
    bool first = true;
    for (double time : downbeats) {
        size_t index = std::lower_bound(beats.begin(), beats.end(), time) - beats.begin();
        if (!first && index > previous)
            spans.push_back(index - previous);
        first = false;
        previous = index;
    }
    if (spans.empty())
        return 1;
    size_t mode = spans.front(), best = 0;
    for (size_t candidate : spans) {
        size_t count = std::count(spans.begin(), spans.end(), candidate);
        if (count > best) {
            mode = candidate;
            best = count;
        }
    }
    size_t consistent = 0;
    for (size_t span : spans)
        if (span == mode || (span % mode == 0 && span <= mode * 4) ||
            (mode % span == 0 && mode <= span * 4))
            ++consistent;
    return double(consistent) / spans.size();
}

} // namespace ytaudio
