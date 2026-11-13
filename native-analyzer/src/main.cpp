#include "CDSPResampler.h"
#include "InferenceProcessor.h"
#include "MelSpectrogram.h"
#include "harmony.hpp"
#include "rhythm.hpp"
#include <cstring>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <nlohmann/json.hpp>
#include <onnxruntime_cxx_api.h>
#include <stdexcept>
#include <thread>
using json = nlohmann::json;
namespace fs = std::filesystem;

void progress(const char *stage, int percent) {
    std::cerr << json{{"stage", stage}, {"percent", percent}}.dump() << '\n';
}
std::vector<float> readPCM(const fs::path &path) {
    auto size = fs::file_size(path);
    constexpr uintmax_t limit = 44100ULL * 2 * 4 * 1800;
    if (size < 44100 * 2 * 4 * 2 || size > limit || size % 8)
        throw std::runtime_error(
            "Audio must be between 2 seconds and 30 minutes of stereo float32 PCM");
    std::vector<float> stereo(size / 4);
    std::ifstream file(path, std::ios::binary);
    if (!file.read(reinterpret_cast<char *>(stereo.data()), size))
        throw std::runtime_error("Cannot read PCM audio");
    double mixedEnergy = 0, leftEnergy = 0, rightEnergy = 0, peak = 0;
    for (size_t i = 0; i < stereo.size(); i += 2) {
        if (!std::isfinite(stereo[i]) || !std::isfinite(stereo[i + 1]))
            throw std::runtime_error("Nonfinite PCM sample");
        double mono = (double(stereo[i]) + double(stereo[i + 1])) * 0.5;
        peak =
            std::max(peak, std::max(std::abs(double(stereo[i])), std::abs(double(stereo[i + 1]))));
        mixedEnergy += mono * mono;
        leftEnergy += double(stereo[i]) * stereo[i];
        rightEnergy += double(stereo[i + 1]) * stereo[i + 1];
    }
    bool cancellation = mixedEnergy < 0.01 * std::max(leftEnergy, rightEnergy);
    std::vector<float> mono(stereo.size() / 2);
    for (size_t i = 0; i < mono.size(); ++i)
        mono[i] = float((cancellation ? double(stereo[2 * i + (rightEnergy > leftEnergy)])
                                      : (double(stereo[2 * i]) + double(stereo[2 * i + 1])) * 0.5) *
                        (peak > 1 ? 1 / peak : 1));
    return mono;
}
std::vector<float> resample(const std::vector<float> &audio, double from, double to) {
    r8b::CDSPResampler24 r(from, to, 4096);
    std::vector<double> block(4096);
    std::vector<float> result;
    const auto expected = static_cast<size_t>(std::ceil(audio.size() * to / from));
    result.reserve(expected + 4096);
    // Flush the resampler delay with zeros; truncate to the original duration.
    size_t position = 0;
    while (result.size() < expected) {
        size_t count =
            std::min<size_t>(4096, audio.size() > position ? audio.size() - position : 4096);
        for (size_t i = 0; i < count; ++i)
            block[i] = position + i < audio.size() ? audio[position + i] : 0;
        double *output = nullptr;
        int n = r.process(block.data(), int(count), output);
        result.insert(result.end(), output, output + n);
        position += count;
        if (position > audio.size() + 65536)
            throw std::runtime_error("Resampling failed");
    }
    result.resize(expected);
    return result;
}
void dumpFloats(const fs::path &p, const std::vector<float> &x) {
    std::ofstream f(p, std::ios::binary);
    f.write(reinterpret_cast<const char *>(x.data()), x.size() * 4);
    if (!f)
        throw std::runtime_error("Cannot write parity data");
}
void dumpTimes(const fs::path &path, const std::vector<double> &values) {
    dumpFloats(path, std::vector<float>(values.begin(), values.end()));
}
int analyzerMain(int argc, char **argv) {
    try {
        fs::path pcm, models, dump;
        int threads = 2;
        for (int i = 1; i < argc; ++i) {
            std::string arg = argv[i];
            if (arg == "--version") {
                std::cout << "ytaudio-analyzer " << YTAUDIO_ENGINE_VERSION << "\n";
                return 0;
            }
            if (i + 1 >= argc)
                throw std::runtime_error("Missing argument");
            if (arg == "--pcm")
                pcm = fs::path(std::u8string(reinterpret_cast<const char8_t *>(argv[++i])));
            else if (arg == "--models")
                models = fs::path(std::u8string(reinterpret_cast<const char8_t *>(argv[++i])));
            else if (arg == "--threads")
                threads = std::stoi(argv[++i]);
            else if (arg == "--dump")
                dump = fs::path(std::u8string(reinterpret_cast<const char8_t *>(argv[++i])));
            else
                throw std::runtime_error("Unknown argument");
        }
        if (pcm.empty() || models.empty() || threads < 1 || threads > 16)
            throw std::runtime_error("Usage: --pcm FILE --models DIRECTORY --threads 1..16");
        progress("Decoding", 5);
        auto mono = readPCM(pcm);
        auto rhythmAudio = resample(mono, 44100, 22050);
        progress("Rhythm preprocessing", 15);
        MelSpectrogram mel;
        auto spect = mel.compute(rhythmAudio);
        if (!dump.empty()) {
            fs::create_directories(dump);
            std::vector<float> flat;
            for (auto &row : spect)
                flat.insert(flat.end(), row.begin(), row.end());
            dumpFloats(dump / "mel.f32", flat);
            dumpFloats(dump / "rhythm-audio.f32", rhythmAudio);
        }
        Ort::Env env(ORT_LOGGING_LEVEL_WARNING, "ytaudio-analyzer");
        env.DisableTelemetryEvents();
        Ort::SessionOptions options;
        options.SetIntraOpNumThreads(threads);
        options.SetInterOpNumThreads(1);
        options.DisableCpuMemArena();
        options.DisableMemPattern();
        options.SetExecutionMode(ExecutionMode::ORT_SEQUENTIAL);
        options.AddConfigEntry("session.intra_op.allow_spinning", "0");
        options.AddConfigEntry("session.inter_op.allow_spinning", "0");
        options.SetGraphOptimizationLevel(GraphOptimizationLevel::ORT_ENABLE_ALL);
        progress("Beat detection", 25);
        std::vector<float> beatLogits, downbeatLogits;
        {
            auto model = models / "beat_this.onnx";
            Ort::Session session(env, model.c_str(), options);
            InferenceProcessor inference(session, env);
            auto logits = inference.process_spectrogram(spect);
            beatLogits = std::move(logits.first);
            downbeatLogits = std::move(logits.second);
        }
        if (!dump.empty()) {
            dumpFloats(dump / "beat-logits.f32", beatLogits);
            dumpFloats(dump / "downbeat-logits.f32", downbeatLogits);
        }
        auto beats = ytaudio::peaks(beatLogits);
        auto downbeats = ytaudio::snapDownbeats(beats, ytaudio::peaks(downbeatLogits));
        auto t = ytaudio::tempo(beats);
        t.support = std::min(t.support, ytaudio::downbeatSupport(beats, downbeats));
        if (!dump.empty()) {
            dumpTimes(dump / "beats.f32", beats);
            dumpTimes(dump / "downbeats.f32", downbeats);
        }
        json result = {
            {"schema_version", 1},
            {"analyzer_version", YTAUDIO_ENGINE_VERSION},
            {"model_version", YTAUDIO_MODELS_VERSION},
            {"tempo",
             {{"bpm", t.bpm > 0 ? json(t.bpm) : json(nullptr)},
              {"alternate_bpm", t.bpm > 0 ? json(t.alternate) : json(nullptr)},
              {"confidence", t.support},
              {"variable", t.variable}}},
            {"key", {{"tonic", nullptr}, {"scale", nullptr}, {"status", "unavailable"}}},
            {"tuning",
             {{"reference_hz", nullptr}, {"offset_cents", nullptr}, {"status", "unknown"}}},
            {"warnings", json::array()}};
        progress("Tuning estimation", 55);
        auto tuning = ytaudio::tuning(rhythmAudio);
        bool normalized = tuning.status == "detuned";
        if (tuning.status == "standard" || normalized) {
            result["tuning"] = {{"status", tuning.status},
                                {"reference_hz", ytaudio::hz(tuning.offset)},
                                {"offset_cents", tuning.offset},
                                {"confidence", tuning.confidence}};
        } else
            result["tuning"]["status"] = tuning.status;
        progress("Key preprocessing", 65);
        auto half = std::move(rhythmAudio);
        auto quarter = resample(half, 22050, 11025);
        // librosa uses energy-scaled recursive downsampling.
        for (auto &x : half)
            x *= std::sqrt(2.0f);
        for (auto &x : quarter)
            x *= 2;
        auto cq = ytaudio::cqt(mono, normalized ? tuning.offset : 0, half, quarter);
        size_t frames = cq[0].size() - 2;
        if (!dump.empty()) {
            std::vector<float> flat;
            for (auto &row : cq)
                flat.insert(flat.end(), row.begin(), row.begin() + frames);
            dumpFloats(dump / "cqt.f32", flat);
        }
        progress("Key detection", 80);
        auto keyModel = models / "keynet.onnx";
        Ort::Session keySession(env, keyModel.c_str(), options);
        Ort::MemoryInfo memory = Ort::MemoryInfo::CreateCpu(OrtArenaAllocator, OrtMemTypeDefault);
        std::array<double, 24> scores{}, voteWeights{};
        // Preserve whole-track coverage while bounding CNN activation memory.
        for (size_t start = 0; start < frames; start += 100) {
            size_t count = std::min<size_t>(100, frames - start), weight = count;
            if (count < 8) {
                start = frames - 8;
                count = 8;
            }
            std::vector<float> tensor(105 * count);
            for (size_t bin = 0; bin < 105; ++bin)
                std::copy_n(cq[bin].begin() + start, count, tensor.begin() + bin * count);
            std::array<int64_t, 4> shape = {1, 1, 105, int64_t(count)};
            auto input = Ort::Value::CreateTensor<float>(memory, tensor.data(), tensor.size(),
                                                         shape.data(), shape.size());
            const char *inputs[] = {"cqt"};
            const char *outputs[] = {"key"};
            auto prediction =
                keySession.Run(Ort::RunOptions{nullptr}, inputs, &input, 1, outputs, 1);
            if (!prediction[0].IsTensor() ||
                prediction[0].GetTensorTypeAndShapeInfo().GetElementCount() != 24)
                throw std::runtime_error("Unexpected key model output");
            auto values = prediction[0].GetTensorData<float>();
            for (int i = 0; i < 24; ++i)
                if (!std::isfinite(values[i]))
                    throw std::runtime_error("Nonfinite key model output");
            int best = int(std::max_element(values, values + 24) - values);
            voteWeights[best] += weight;
            for (int i = 0; i < 24; ++i)
                scores[i] += values[i] * weight;
            if (start + count == frames)
                break;
        }
        std::array<int, 24> ranking;
        std::iota(ranking.begin(), ranking.end(), 0);
        std::sort(ranking.begin(), ranking.end(),
                  [&](int a, int b) { return scores[a] > scores[b]; });
        int best = ranking[0], second = ranking[1];
        double agreement = voteWeights[best] / frames;
        double margin = (scores[best] - scores[second]) / frames;
        double chromaTotal = std::accumulate(tuning.chroma.begin(), tuning.chroma.end(), 0.0);
        const std::array<int, 7> major = {0, 2, 4, 5, 7, 9, 11}, minor = {0, 2, 3, 5, 7, 8, 10};
        double inScale = 0;
        for (int step : best < 12 ? minor : major)
            inScale += tuning.chroma[(ytaudio::tonicPitch[best] + step) % 12];
        bool uncertain = std::abs(tuning.offset) > 47 || tuning.status == "unknown" ||
                         tuning.status == "mixed" || agreement < 0.6 || margin < 0.2 ||
                         (chromaTotal > 0 && inScale / chromaTotal < 0.65);
        double strongestPitch = *std::max_element(tuning.chroma.begin(), tuning.chroma.end());
        int pitchedClasses = std::count_if(
            tuning.chroma.begin(), tuning.chroma.end(), [strongestPitch](double weight) {
                return strongestPitch > 0 && weight > strongestPitch * 0.1;
            });
        // Noise and one sustained note cannot establish a major/minor scale.
        // Stable FFT-bin coincidences alone are insufficient pitched evidence.
        if (tuning.status != "unknown" && chromaTotal > 0 && tuning.frames >= 8 &&
            pitchedClasses >= 3) {
            result["key"] = {
                {"tonic", ytaudio::tonics[best]},
                {"scale", best < 12 ? "minor" : "major"},
                {"status", uncertain ? "uncertain" : "estimated"},
                {"tuning_normalized", normalized},
                {"confidence", agreement},
                {"alternate",
                 {{"tonic", ytaudio::tonics[second]}, {"scale", second < 12 ? "minor" : "major"}}}};
        }
        if (!dump.empty()) {
            std::vector<float> logits(scores.begin(), scores.end());
            for (auto &x : logits)
                x /= frames;
            dumpFloats(dump / "key-logits.f32", logits);
        }
        progress("Complete", 100);
        std::cout << result.dump() << '\n';
        return 0;
    } catch (const std::exception &e) {
        std::cerr << json{{"error", e.what()}}.dump() << '\n';
        return 1;
    }
}

#ifdef _WIN32
int wmain(int argc, wchar_t **argv) {
    std::vector<std::string> text;
    std::vector<char *> args;
    for (int i = 0; i < argc; ++i) {
        auto utf8 = fs::path(argv[i]).u8string();
        text.emplace_back(reinterpret_cast<const char *>(utf8.c_str()));
    }
    for (auto &item : text)
        args.push_back(item.data());
    return analyzerMain(argc, args.data());
}
#else
int main(int argc, char **argv) {
    return analyzerMain(argc, argv);
}
#endif
