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
    internal static void Perform(string source, string json, bool sessionOwned = false)
    {
        if (source != "primary") throw new ArgumentException("Pointer requires the approved primary display");
        using var document = JsonDocument.Parse(json);
        var root = document.RootElement;
        var operation = root.GetProperty("operation").GetString();
        if ((operation == "pointer.down" || operation == "pointer.up") && !sessionOwned) throw new ArgumentException("Held input requires a session");
        var x = root.GetProperty("x").GetDouble(); var y = root.GetProperty("y").GetDouble();
        var dx = root.GetProperty("deltaX").GetInt32(); var dy = root.GetProperty("deltaY").GetInt32();
        if (!Double.IsFinite(x) || !Double.IsFinite(y) || x < 0 || x > 1 || y < 0 || y > 1 || dx < -100 || dx > 100 || dy < -100 || dy > 100) throw new ArgumentException("Invalid pointer request");
        if (!SetCursorPos((int)(x * (GetSystemMetrics(0)-1)), (int)(y * (GetSystemMetrics(1)-1)))) throw new IOException("Pointer unavailable in this desktop");
        Input[] events;
        switch (root.GetProperty("operation").GetString())
        {
            case "pointer.move": return;
            case "pointer.click":
            case "pointer.down":
            case "pointer.up":
                events = root.GetProperty("button").GetString() switch {
                    "left" => new[] { Event(2), Event(4) }, "right" => new[] { Event(8), Event(16) },
                    "middle" => new[] { Event(32), Event(64) }, _ => throw new ArgumentException("Invalid button") };
                if (root.GetProperty("operation").GetString() == "pointer.down") events = new[] { events[0] };
                if (root.GetProperty("operation").GetString() == "pointer.up") events = new[] { events[1] };
                break;
            case "pointer.scroll": events = new[] { Event(0x800, dy*120), Event(0x1000, dx*120) }; break;
            default: throw new ArgumentException("Unsupported pointer operation");
        }
        if (SendInput((uint)events.Length, events, Marshal.SizeOf<Input>()) != events.Length)
        {
            if (operation == "pointer.click" || operation == "pointer.down" || operation == "pointer.up") Release(root.GetProperty("button").GetString() ?? "");
            throw new IOException("Pointer injection rejected");
        }
    }
    private static void Release(string button)
    {
        if (button.Length == 0) return;
        var release = new[] { Event(button == "left" ? 4u : button == "right" ? 16u : 64u) };
        if (SendInput(1, release, Marshal.SizeOf<Input>()) != 1) throw new IOException("Button release rejected");
    }
    internal static void Session(string source)
    {
        string held = "";
        try {
            while (true) {
                var pending = Task.Run(() => Console.ReadLine());
                if (held.Length > 0 && !pending.Wait(TimeSpan.FromSeconds(5))) throw new IOException("Held input idle timeout");
                var text = pending.GetAwaiter().GetResult();
                if (text == null) return;
                if (text.Length > 4096) throw new ArgumentException("Pointer request too large");
                using var document = JsonDocument.Parse(text);
                var root = document.RootElement;
                var operation = root.GetProperty("operation").GetString();
                var button = root.GetProperty("button").GetString() ?? "";
                switch (operation) {
                    case "pointer.release": Release(held); held = ""; break;
                    case "pointer.down":
                        if (held.Length != 0 || (button != "left" && button != "right" && button != "middle")) throw new ArgumentException("Invalid hold");
                        held = button; Perform(source, text, true); break;
                    case "pointer.up":
                        if (held.Length != 0 && held != button) throw new ArgumentException("Button mismatch");
                        if (held.Length != 0) { Perform(source, text, true); held = ""; } break;
                    case "pointer.move": Perform(source, text, true); break;
                    case "pointer.click": case "pointer.scroll":
                        if (held.Length != 0) throw new ArgumentException("Release held button first");
                        Perform(source, text, true); break;
                    default: throw new ArgumentException("Unknown pointer operation");
                }
                Console.Out.WriteLine("ok"); Console.Out.Flush();
            }
        } finally { Release(held); }
    }

}
