#include "rhythm.hpp"
#include <iostream>
#include <stdexcept>
void require(bool condition, const char *message) {
    if (!condition)
        throw std::runtime_error(message);
}
int main() {
    std::vector<double> beats;
    for (int i = 0; i < 80; ++i)
        if (i != 20 && i != 50)
            beats.push_back(float(i * 60.0 / 140));
    auto t = ytaudio::tempo(beats);
    require(std::abs(t.bpm - 140) < 0.01, "missed-beat tempo");
    require(std::abs(t.alternate - 70) < 0.01, "tempo alternative");
    require(!t.variable, "constant tempo");
    require(ytaudio::tempo({0, 1, 2}).bpm == 0, "insufficient beats");
    beats.clear();
    for (int i = 0; i < 100; ++i)
        beats.push_back(std::round(i * 60.0 / 140 * 50) / 50);
    require(std::abs(ytaudio::tempo(beats).bpm - 140) < 0.1, "frame-quantized tempo");
    beats.clear();
    double time = 0;
    for (int i = 0; i < 90; ++i) {
        beats.push_back(time);
        time += i < 45 ? 0.5 : 0.7;
    }
    require(ytaudio::tempo(beats).variable, "variable tempo");
    auto peaks = ytaudio::peaks({-1, 2, 2, -1, -1, -1, -1, 3, -1});
    require(peaks.size() == 2 && std::abs(peaks[0] - 0.03) < 0.001, "peak pooling/tie rounding");
    auto down = ytaudio::snapDownbeats({0.02, 0.04, 0.14}, {0.03, 0.135, 0.145});
    require(down.size() == 2 && std::abs(down[0] - 0.02) < 1e-10,
            "downbeat nearest-beat alignment");
    beats.clear();
    time = 0;
    for (int i = 0; i < 80; ++i) {
        beats.push_back(time);
        time += (i / 16) % 2 ? 1.0 : 0.5;
    }
    require(!ytaudio::tempo(beats).variable,
            "missing beat sections are octave-equivalent, not necessarily variable tempo");
    beats.clear();
    time = 0;
    for (int i = 0; i < 256; ++i) {
        beats.push_back(time);
        // Two eight-interval sections contain equal numbers of single/double
        // intervals. Their raw median is 0.75 s despite a constant 0.5 s pulse.
        bool missed = (i >= 64 && i < 68) || (i >= 128 && i < 132);
        time += missed ? 1.0 : 0.5;
    }
    t = ytaudio::tempo(beats);
    require(!t.variable && std::abs(t.bpm - 120) < 0.01,
            "mixed single/double intervals must not invent a tempo change");
    std::cout << "rhythm tests passed\n";
}
