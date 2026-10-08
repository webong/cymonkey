using NAudio.CoreAudioApi;
using NAudio.Wave;
using Cymonkey.Windows.Media;

// Process protocol: capture emits a streaming WAV; playback consumes PCM16
// stereo 48 kHz. The Board parent owns this process and the format converter.
internal static class Program
{
    private static int Main(string[] args)
    {
        try
        {
            if (args.Length != 2) throw new ArgumentException("Expected mode and source");
            var mode = args[0];
            var source = args[1];
            if (mode == "describe")
            {
                var endpoints = DeviceAudio.Endpoints();
                Console.Write(System.Text.Json.JsonSerializer.Serialize(new {
                    platform = "windows", systemCapture = "checked-on-open", microphone = "checked-on-open", playback = "checked-on-open",
                    systemSources = endpoints.Where(e => e.Playback).Select(e => e.Id),
                    microphoneSources = endpoints.Where(e => !e.Playback).Select(e => e.Id),
                    playbackSources = endpoints.Where(e => e.Playback).Select(e => e.Id)
                }));
                return 0;
            }
            if (mode != "capture-system" && mode != "capture-microphone" && mode != "playback")
                throw new ArgumentException("Unsupported audio mode");
            if (String.IsNullOrEmpty(source) || source == "default")
            {
                using var enumerator = new MMDeviceEnumerator();
                using var endpoint = enumerator.GetDefaultAudioEndpoint(mode == "capture-microphone" ? DataFlow.Capture : DataFlow.Render, Role.Console);
                source = endpoint.ID;
            }
            if (mode == "playback")
            {
                using var output = DeviceAudio.Playback(source, new WaveFormat(48000, 16, 2));
                using var input = Console.OpenStandardInput();
                var bytes = new byte[3840];
                var pending = 0;
                while (true)
                {
                    var n = input.Read(bytes, pending, bytes.Length - pending);
                    if (n == 0) { if (pending != 0) throw new IOException("Truncated PCM frame"); return 0; }
                    var total = pending + n;
                    var count = total / 4 * 4;
                    if (count > 0)
                    {
                        output.Write(bytes, 0, count);
                        Thread.Sleep(TimeSpan.FromSeconds((double)count / 192000));
                    }
                    pending = total - count;
                    Array.Copy(bytes, count, bytes, 0, pending);
                }
            }
            using var capture = DeviceAudio.Capture(source, mode == "capture-system");
            using var stream = Console.OpenStandardOutput();
            using var done = new ManualResetEventSlim();
            var format = capture.Format;
            if (format.Encoding != WaveFormatEncoding.Pcm && format.Encoding != WaveFormatEncoding.IeeeFloat)
                throw new IOException("Unsupported native capture format");
            using (var header = new BinaryWriter(stream, System.Text.Encoding.ASCII, leaveOpen: true))
            {
                header.Write(System.Text.Encoding.ASCII.GetBytes("RIFF")); header.Write(UInt32.MaxValue);
                header.Write(System.Text.Encoding.ASCII.GetBytes("WAVEfmt ")); header.Write(16);
                header.Write((ushort)format.Encoding); header.Write((ushort)format.Channels);
                header.Write(format.SampleRate); header.Write(format.AverageBytesPerSecond);
                header.Write((ushort)format.BlockAlign); header.Write((ushort)format.BitsPerSample);
                header.Write(System.Text.Encoding.ASCII.GetBytes("data")); header.Write(UInt32.MaxValue);
            }
            Exception? failure = null;
            capture.Samples += bytes => stream.Write(bytes.Span);
            capture.Stopped += error => { failure = error; done.Set(); };
            capture.Start(); done.Wait();
            if (failure != null) throw failure;
            return 0;
        }
        catch (Exception error) { Console.Error.WriteLine($"Native audio unavailable: {error.Message}"); return 2; }
    }
}
