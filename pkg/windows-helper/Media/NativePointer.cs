using System.Runtime.InteropServices;
using System.Text.Json;
namespace Cymonkey.Windows.Media;
internal static class NativePointer
{
    [StructLayout(LayoutKind.Sequential)] private struct Mouse { public int X, Y; public uint Data, Flags, Time; public UIntPtr Extra; }
    [StructLayout(LayoutKind.Explicit)] private struct Union { [FieldOffset(0)] public Mouse Mouse; }
    [StructLayout(LayoutKind.Sequential)] private struct Input { public uint Type; public Union Value; }
    [DllImport("user32.dll", SetLastError=true)] private static extern bool SetCursorPos(int x, int y);
    [DllImport("user32.dll")] private static extern int GetSystemMetrics(int index);
    [DllImport("user32.dll", SetLastError=true)] private static extern uint SendInput(uint count, Input[] inputs, int size);
    private static Input Event(uint flags, int data = 0) => new() { Value = new Union { Mouse = new Mouse { Flags = flags, Data = unchecked((uint)data) } } };
    internal static void Perform(string source, string json)
    {
        if (source != "primary") throw new ArgumentException("Pointer requires the approved primary display");
        using var document = JsonDocument.Parse(json);
        var root = document.RootElement;
        var x = root.GetProperty("x").GetDouble(); var y = root.GetProperty("y").GetDouble();
        var dx = root.GetProperty("deltaX").GetInt32(); var dy = root.GetProperty("deltaY").GetInt32();
        if (!Double.IsFinite(x) || !Double.IsFinite(y) || x < 0 || x > 1 || y < 0 || y > 1 || dx < -100 || dx > 100 || dy < -100 || dy > 100) throw new ArgumentException("Invalid pointer request");
        if (!SetCursorPos((int)(x * (GetSystemMetrics(0)-1)), (int)(y * (GetSystemMetrics(1)-1)))) throw new IOException("Pointer unavailable in this desktop");
        Input[] events;
        switch (root.GetProperty("operation").GetString())
        {
            case "pointer.move": return;
            case "pointer.click":
                events = root.GetProperty("button").GetString() switch {
                    "left" => new[] { Event(2), Event(4) }, "right" => new[] { Event(8), Event(16) },
                    "middle" => new[] { Event(32), Event(64) }, _ => throw new ArgumentException("Invalid button") };
                break;
            case "pointer.scroll": events = new[] { Event(0x800, dy*120), Event(0x1000, dx*120) }; break;
            default: throw new ArgumentException("Unsupported pointer operation");
        }
        if (SendInput((uint)events.Length, events, Marshal.SizeOf<Input>()) != events.Length)
        {
            var releases = new[] { Event(4), Event(16), Event(64) };
            SendInput(3, releases, Marshal.SizeOf<Input>());
            throw new IOException("Pointer injection rejected");
        }
    }
}
