using NAudio.CoreAudioApi;
using NAudio.Wave;

namespace Cymonkey.Windows.Media;

public sealed record AudioEndpoint(string Id, string Name, bool Playback);

/// WASAPI audio for a host-approved endpoint. Host enforces independent system
/// capture, microphone and playback grants before constructing these objects.
public static class DeviceAudio
{
    public static IReadOnlyList<AudioEndpoint> Endpoints()
    {
        using var enumerator = new MMDeviceEnumerator();
        var result = new List<AudioEndpoint>();
        foreach (var flow in new[] { DataFlow.Render, DataFlow.Capture })
            foreach (var device in enumerator.EnumerateAudioEndPoints(flow, DeviceState.Active))
                using (device) result.Add(new(device.ID, device.FriendlyName, flow == DataFlow.Render));
        return result;
    }

    public static AudioCapture Capture(string approvedEndpointId, bool systemAudio) => new(approvedEndpointId, systemAudio);
    public static AudioPlayback Playback(string approvedEndpointId, WaveFormat format) => new(approvedEndpointId, format);
}

/// Bytes are in Format, not necessarily PCM16. Host must normalize/encode before
/// WebRTC. Callback memory is valid only until return; keep callbacks bounded.
public sealed class AudioCapture : IDisposable
{
    private readonly MMDevice device;
    private readonly WasapiCapture capture;
    private bool disposed;
    public WaveFormat Format => capture.WaveFormat;
    public event Action<ReadOnlyMemory<byte>>? Samples;
    public event Action<Exception?>? Stopped;
    internal AudioCapture(string endpoint, bool systemAudio)
    {
        using var enumerator = new MMDeviceEnumerator();
        device = enumerator.GetDevice(endpoint);
        try
        {
            if (device.DataFlow != (systemAudio ? DataFlow.Render : DataFlow.Capture))
                throw new ArgumentException("Endpoint direction does not match the requested grant");
            capture = systemAudio ? new WasapiLoopbackCapture(device) : new WasapiCapture(device);
            capture.DataAvailable += (_, e) => Samples?.Invoke(e.Buffer.AsMemory(0, e.BytesRecorded));
            capture.RecordingStopped += (_, e) => Stopped?.Invoke(e.Exception);
        }
        catch { device.Dispose(); throw; }
    }
    public void Start() { ObjectDisposedException.ThrowIf(disposed, this); capture.StartRecording(); }
    public void Dispose()
    {
        if (disposed) return; disposed = true;
        try { capture.StopRecording(); } finally { capture.Dispose(); device.Dispose(); }
    }
}

/// Bounded native playback; Write reports backpressure instead of silently
/// accumulating delayed audio. A single session thread owns Write/Dispose.
public sealed class AudioPlayback : IDisposable
{
    private readonly MMDevice device;
    private readonly WasapiOut output;
    private readonly BufferedWaveProvider buffer;
    private bool disposed;
    internal AudioPlayback(string endpoint, WaveFormat format)
    {
        if (format.Encoding != WaveFormatEncoding.Pcm || format.BitsPerSample != 16 ||
            format.SampleRate != 48000 || format.Channels != 2)
            throw new ArgumentException("Playback requires PCM16 stereo 48 kHz");
        using var enumerator = new MMDeviceEnumerator();
        device = enumerator.GetDevice(endpoint);
        WasapiOut? created = null;
        try
        {
            if (device.DataFlow != DataFlow.Render) throw new ArgumentException("Playback needs a render endpoint");
            buffer = new BufferedWaveProvider(format) { BufferDuration = TimeSpan.FromMilliseconds(250), DiscardOnBufferOverflow = false };
            created = new WasapiOut(device, AudioClientShareMode.Shared, true, 60);
            created.Init(buffer); created.Play(); output = created;
        }
        catch { created?.Dispose(); device.Dispose(); throw; }
    }
    public void Write(byte[] bytes, int offset, int count)
    {
        ObjectDisposedException.ThrowIf(disposed, this);
        if (count % buffer.WaveFormat.BlockAlign != 0) throw new ArgumentException("Incomplete PCM frame");
        if (count > buffer.BufferLength - buffer.BufferedBytes) throw new IOException("Playback buffer full");
        buffer.AddSamples(bytes, offset, count);
    }
    public void Dispose()
    {
        if (disposed) return; disposed = true;
        try { output.Stop(); } finally { output.Dispose(); buffer.ClearBuffer(); device.Dispose(); }
    }
}
