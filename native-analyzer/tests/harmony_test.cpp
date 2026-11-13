#include "harmony.hpp"
#include <iostream>
#include <stdexcept>
void require(bool condition, const char *message) {
    if (!condition)
        throw std::runtime_error(message);
}
std::vector<float> chord(double offset, bool mixed = false) {
    std::vector<float> signal(22050 * 6);
    for (size_t i = 0; i < signal.size(); ++i) {
        double t = i / 22050.0;
        for (double f : {130.8128, 164.8138, 195.9977, 261.6256, 329.6276, 391.9954}) {
            signal[i] += float(0.1 * std::sin(2 * ytaudio::pi * f * std::exp2(offset / 1200) * t));
            if (mixed)
                signal[i] += float(
                    0.1 * std::sin(2 * ytaudio::pi * f * std::exp2((offset + 30) / 1200) * t));
        }
    }
    return signal;
}
int main() {
    require(std::abs(ytaudio::cents(432) + 31.76665) < 0.0001, "432 Hz cents");
    require(std::abs(ytaudio::hz(-31.76665) - 432) < 0.0001, "cents to reference");
    require(std::abs(ytaudio::circularDistance(49, -49) + 2) < 0.001, "circular boundary");
    for (double offset : {0.0, 1.0, -31.76665, 23.0, 48.0, -48.0}) {
        auto t = ytaudio::tuning(chord(offset));
        std::cout << offset << " -> " << t.offset << " " << t.status << "\n";
        require(t.status == "standard" || t.status == "detuned",
                "pitched signal has a tuning estimate");
        require(std::abs(ytaudio::circularDistance(offset, t.offset)) < 2,
                "controlled tuning error below 2 cents");
    }
    // Check both sides of the standard-tuning tolerance with actual audio.
    for (double offset : {-3.0, -2.0, 2.0, 3.0}) {
        auto estimate = ytaudio::tuning(chord(offset));
        require(estimate.status == (std::abs(offset) < 2.5 ? "standard" : "detuned"),
                "near-standard tuning threshold");
    }
    require(ytaudio::tuning(std::vector<float>(22050 * 6)).status == "unknown", "silence unknown");
    auto mixed = ytaudio::tuning(chord(-15, true));
    require(mixed.status == "mixed", "conflicting tuning is mixed");
    std::array<double, 100> histogram{};
    for (int i = 0; i < 100; ++i) {
        double offset = i - 50;
        // A standard-tuned cluster with broad detuned shoulders and a diffuse
        // floor, as can occur with effects/partials, is not two tuning systems.
        histogram[i] = 8 + 170 * std::exp(-0.5 * offset * offset / 16) +
                       85 * std::exp(-0.5 * std::pow((offset - 10) / 4, 2)) +
                       75 * std::exp(-0.5 * std::pow((offset + 10) / 4, 2));
    }
    ytaudio::Tuning shoulder;
    ytaudio::summarizeTuning(shoulder, histogram);
    require(shoulder.status == "standard", "unresolved shoulders are not mixed tuning");
    histogram.fill(1);
    ytaudio::Tuning noise;
    ytaudio::summarizeTuning(noise, histogram);
    require(noise.status == "unknown", "uniform offsets remain unknown after floor removal");
    for (int i = 0; i < 100; ++i) {
        double offset = i - 50;
        histogram[i] = (std::abs(offset) < 20 ? 4 : 0) +
                       10 * std::exp(-0.5 * offset * offset / 4);
    }
    ytaudio::Tuning diffuse;
    ytaudio::summarizeTuning(diffuse, histogram);
    require(diffuse.status == "unknown", "diffuse pitch offsets do not prove mixed tuning");
    // Check separated references across the circular +/-50-cent boundary too.
    require(ytaudio::tuning(chord(35, true)).status == "mixed",
            "competing tunings across circular boundary");
    require(std::string(ytaudio::tonics[10]) == "F#" && ytaudio::tonicPitch[10] == 6,
            "Camelot 11A mapping");
    require(std::string(ytaudio::tonics[16]) == "Eb" && ytaudio::tonicPitch[16] == 3,
            "Camelot 5B mapping");
    const std::array<const char *, 12> names = {"C",  "C#", "D",  "Eb", "E",  "F",
                                                "F#", "G",  "Ab", "A",  "Bb", "B"};
    for (int i = 0; i < 12; ++i) {
        int minor = (8 + 7 * i) % 12, major = (minor + 3) % 12;
        require(ytaudio::tonicPitch[i] == minor && ytaudio::tonicPitch[i + 12] == major,
                "all 24 Camelot classes and relative-key mapping");
        require(std::string(ytaudio::tonics[i]) == names[minor] &&
                    std::string(ytaudio::tonics[i + 12]) == names[major],
                "canonical enharmonic names");
    }
}
