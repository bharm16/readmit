# The Windows backend of tools/native_journey.py: reads and acts on the
# installed application through UI Automation, one JSON request per line on
# standard input and one JSON answer per line on standard output. Every
# decision about what a journey expects is made by the caller. The backend is
# compiled from the C# below by Windows PowerShell's own Add-Type, against the
# .NET Framework's UI Automation client; nothing is downloaded.
$ErrorActionPreference = 'Stop'
$source = @'
using System;
using System.Collections;
using System.Collections.Generic;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;
using System.Web.Script.Serialization;
using System.Windows.Automation;

public static class NativeBackend {
    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    static extern IntPtr SendMessage(IntPtr window, uint message, IntPtr wParam, string lParam);
    [DllImport("user32.dll")]
    static extern bool PostMessage(IntPtr window, uint message, IntPtr wParam, IntPtr lParam);
    const uint SetText = 0x000C;
    const uint Click = 0x00F5;

    [StructLayout(LayoutKind.Sequential)]
    struct KeyboardInput { public ushort Key; public ushort Scan; public uint Flags; public uint Time; public IntPtr Extra; }
    [StructLayout(LayoutKind.Sequential)]
    struct MouseInput { public int X; public int Y; public uint Data; public uint Flags; public uint Time; public IntPtr Extra; }
    [StructLayout(LayoutKind.Explicit)]
    struct InputUnion { [FieldOffset(0)] public KeyboardInput Keyboard; [FieldOffset(0)] public MouseInput Mouse; }
    [StructLayout(LayoutKind.Sequential)]
    struct Input { public uint Type; public InputUnion Union; }
    [DllImport("user32.dll", SetLastError = true)]
    static extern uint SendInput(uint count, Input[] inputs, int size);
    const uint KeyUp = 0x0002, Unicode = 0x0004;
    const uint MouseMove = 0x0001, MouseDown = 0x0002, MouseUp = 0x0004, MouseAbsolute = 0x8000;
    [DllImport("user32.dll")]
    static extern IntPtr GetForegroundWindow();
    [DllImport("user32.dll")]
    static extern int GetSystemMetrics(int index);
    const ushort Control = 0x11, LetterA = 0x41, Delete = 0x2E;

    static Input Key(ushort key, ushort scan, uint flags) {
        var input = new Input { Type = 1 };
        input.Union.Keyboard = new KeyboardInput { Key = key, Scan = scan, Flags = flags };
        return input;
    }

    // What a person types reaches the page as key presses, so the page sees
    // the input events typing makes: select everything the field holds,
    // delete it, then type the text. Keys reach only the window in front, so
    // the window is brought forward first, as a press does, and should Windows
    // keep another window in front, the field is clicked, as a person clicks
    // into a field before typing.
    static void Type(AutomationElement field, string text) {
        var window = Focus(field);
        Thread.Sleep(150);
        if (window != IntPtr.Zero && GetForegroundWindow() != window) {
            var bounds = field.Current.BoundingRectangle;
            var click = new Input[3];
            for (int index = 0; index < 3; index++) click[index] = new Input { Type = 0 };
            click[0].Union.Mouse = new MouseInput {
                X = (int)((bounds.Left + Math.Min(bounds.Width / 2, 40)) * 65535 / GetSystemMetrics(0)),
                Y = (int)((bounds.Top + bounds.Height / 2) * 65535 / GetSystemMetrics(1)),
                Flags = MouseMove | MouseAbsolute,
            };
            click[1].Union.Mouse = new MouseInput { Flags = MouseDown };
            click[2].Union.Mouse = new MouseInput { Flags = MouseUp };
            SendInput(3, click, Marshal.SizeOf(typeof(Input)));
            Thread.Sleep(300);
            if (GetForegroundWindow() != window) throw new Failure("the window is not in front, so no keys were sent");
        }
        if (!field.Current.HasKeyboardFocus) throw new Failure("the field did not take the keyboard focus, so nothing was typed");
        var keys = new List<Input> {
            Key(Control, 0, 0), Key(LetterA, 0, 0), Key(LetterA, 0, KeyUp), Key(Control, 0, KeyUp),
            Key(Delete, 0, 0), Key(Delete, 0, KeyUp),
        };
        foreach (char character in text) {
            keys.Add(Key(0, character, Unicode));
            keys.Add(Key(0, character, Unicode | KeyUp));
        }
        var inputs = keys.ToArray();
        if (SendInput((uint)inputs.Length, inputs, Marshal.SizeOf(typeof(Input))) != inputs.Length) {
            throw new Failure("the keys were not delivered: " + Marshal.GetLastWin32Error());
        }
        Thread.Sleep(200);
    }

    static readonly JavaScriptSerializer Json = new JavaScriptSerializer { MaxJsonLength = int.MaxValue };
    static Dictionary<int, AutomationElement> handles = new Dictionary<int, AutomationElement>();

    class Failure : Exception { public Failure(string message) : base(message) { } }

    static List<AutomationElement> Windows(int pid) {
        var condition = new PropertyCondition(AutomationElement.ProcessIdProperty, pid);
        var list = new List<AutomationElement>();
        foreach (AutomationElement window in AutomationElement.RootElement.FindAll(TreeScope.Children, condition)) list.Add(window);
        return list;
    }

    static object Snapshot(int pid) {
        handles = new Dictionary<int, AutomationElement>();
        var nodes = new List<object>();
        var request = new CacheRequest();
        request.TreeFilter = Automation.ControlViewCondition;
        request.TreeScope = TreeScope.Element | TreeScope.Descendants;
        request.Add(AutomationElement.ControlTypeProperty);
        request.Add(AutomationElement.NameProperty);
        request.Add(AutomationElement.IsEnabledProperty);
        request.Add(AutomationElement.HasKeyboardFocusProperty);
        request.Add(AutomationElement.ClassNameProperty);
        request.Add(ValuePattern.ValueProperty);
        request.Add(TogglePattern.ToggleStateProperty);
        using (request.Activate()) {
            foreach (var window in Windows(pid)) {
                AutomationElement cached;
                try { cached = window.GetUpdatedCache(request); } catch (ElementNotAvailableException) { continue; }
                Walk(cached, -1, 0, nodes);
            }
        }
        return nodes;
    }

    static void Walk(AutomationElement element, int parent, int depth, List<object> nodes) {
        if (nodes.Count >= 6000 || depth > 90) return;
        int id = nodes.Count;
        handles[id] = element;
        var node = new Dictionary<string, object>();
        node["id"] = id;
        if (parent >= 0) node["parent"] = parent;
        string role = ((ControlType)element.GetCachedPropertyValue(AutomationElement.ControlTypeProperty)).ProgrammaticName.Replace("ControlType.", "");
        string name = (string)element.GetCachedPropertyValue(AutomationElement.NameProperty) ?? "";
        node["role"] = role;
        node["class"] = (string)element.GetCachedPropertyValue(AutomationElement.ClassNameProperty) ?? "";
        node["enabled"] = (bool)element.GetCachedPropertyValue(AutomationElement.IsEnabledProperty);
        node["focused"] = (bool)element.GetCachedPropertyValue(AutomationElement.HasKeyboardFocusProperty);
        if (role == "Text") { node["text"] = name; node["name"] = ""; } else { node["name"] = name; }
        object value = element.GetCachedPropertyValue(ValuePattern.ValueProperty, true);
        if (value is string && role != "Text" && role != "Document") node["value"] = value;
        object toggle = element.GetCachedPropertyValue(TogglePattern.ToggleStateProperty, true);
        if (toggle is ToggleState) node["checked"] = (ToggleState)toggle == ToggleState.On;
        nodes.Add(node);
        foreach (AutomationElement child in element.CachedChildren) Walk(child, id, depth + 1, nodes);
    }

    static AutomationElement Element(IDictionary<string, object> request) {
        AutomationElement found;
        if (!request.ContainsKey("id") || !handles.TryGetValue(Convert.ToInt32(request["id"]), out found)) throw new Failure("no such element in the last snapshot");
        return found;
    }

    [DllImport("user32.dll")]
    static extern bool SetForegroundWindow(IntPtr window);

    // A person's press first brings the window forward and puts the focus on
    // the control; a dialog the press opens then takes the focus from the
    // window rather than finding none to take.
    static IntPtr Focus(AutomationElement element) {
        var walker = TreeWalker.ControlViewWalker;
        AutomationElement top = element;
        for (var parent = walker.GetParent(top); parent != null && parent != AutomationElement.RootElement; parent = walker.GetParent(parent)) top = parent;
        var window = new IntPtr(top.Current.NativeWindowHandle);
        if (window != IntPtr.Zero) SetForegroundWindow(window);
        try { element.SetFocus(); } catch (InvalidOperationException) { }
        Thread.Sleep(100);
        return window;
    }

    static void Press(AutomationElement element) {
        object pattern;
        Focus(element);
        var type = element.Current.ControlType;
        if (type == ControlType.CheckBox && element.TryGetCurrentPattern(TogglePattern.Pattern, out pattern)) { ((TogglePattern)pattern).Toggle(); return; }
        if (type == ControlType.TabItem && element.TryGetCurrentPattern(SelectionItemPattern.Pattern, out pattern)) { ((SelectionItemPattern)pattern).Select(); return; }
        if (element.TryGetCurrentPattern(InvokePattern.Pattern, out pattern)) { ((InvokePattern)pattern).Invoke(); return; }
        // A disclosure summary opens or closes what it summarizes.
        if (element.TryGetCurrentPattern(ExpandCollapsePattern.Pattern, out pattern)) {
            var disclosure = (ExpandCollapsePattern)pattern;
            if (disclosure.Current.ExpandCollapseState == ExpandCollapseState.Collapsed) disclosure.Expand(); else disclosure.Collapse();
            return;
        }
        if (element.TryGetCurrentPattern(TogglePattern.Pattern, out pattern)) { ((TogglePattern)pattern).Toggle(); return; }
        throw new Failure("the element offers no action a person's press performs");
    }

    static AutomationElement Dialog(int pid, string title, double seconds) {
        DateTime deadline = DateTime.UtcNow.AddSeconds(seconds);
        do {
            foreach (var window in Windows(pid)) {
                // A window closing while it is read is not the dialog.
                try {
                    if (window.Current.Name == title) return window;
                    var inner = window.FindFirst(TreeScope.Descendants, new PropertyCondition(AutomationElement.NameProperty, title));
                    if (inner != null && inner.Current.ClassName == "#32770") return inner;
                } catch (ElementNotAvailableException) {
                } catch (InvalidOperationException) {
                }
            }
            Thread.Sleep(300);
        } while (DateTime.UtcNow < deadline);
        return null;
    }

    // The host's folder picker is answered as a person answers it: the path
    // typed into its Folder field, then its Select Folder button. The field
    // is a plain edit control UI Automation exposes without a value pattern,
    // so the text reaches it as a window message.
    static void ChooseFolder(int pid, string title, string path, double seconds) {
        var dialog = Dialog(pid, title, seconds);
        if (dialog == null) throw new Failure("no folder dialog titled '" + title + "' opened");
        AutomationElement field = null;
        foreach (AutomationElement candidate in dialog.FindAll(TreeScope.Descendants, new PropertyCondition(AutomationElement.ClassNameProperty, "Edit"))) {
            if (candidate.Current.NativeWindowHandle != 0) field = candidate;
        }
        var button = DialogButton(dialog, "Select Folder");
        if (field == null || button == null) throw new Failure("the folder dialog offered no Folder field or Select Folder button");
        EnterAndAccept(pid, title, field, button, path, "the folder dialog stayed open after choosing ");
    }

    // The host's save dialog is answered as a person answers it: the new
    // folder's whole path typed into its File name field, then its Save
    // button. The dialog creates nothing; the writer it answers does.
    static void NameNewFolder(int pid, string title, string path, double seconds) {
        var dialog = Dialog(pid, title, seconds);
        if (dialog == null) throw new Failure("no save dialog titled '" + title + "' opened");
        AutomationElement field = null;
        foreach (AutomationElement candidate in dialog.FindAll(TreeScope.Descendants, new PropertyCondition(AutomationElement.ClassNameProperty, "Edit"))) {
            if (candidate.Current.NativeWindowHandle == 0) continue;
            // 1001 is the control id of the File name field's edit control.
            if (candidate.Current.AutomationId == "1001") { field = candidate; break; }
            field = candidate;
        }
        var button = DialogButton(dialog, "Save");
        if (field == null || button == null) throw new Failure("the save dialog offered no File name field or Save button");
        EnterAndAccept(pid, title, field, button, path, "the save dialog stayed open after naming ");
    }

    static AutomationElement DialogButton(AutomationElement dialog, string name) {
        AutomationElement button = null;
        foreach (AutomationElement candidate in dialog.FindAll(TreeScope.Descendants, new PropertyCondition(AutomationElement.ClassNameProperty, "Button"))) {
            if (candidate.Current.Name == name && candidate.Current.NativeWindowHandle != 0) button = candidate;
        }
        return button;
    }

    // A dialog still initializing can overwrite its field, so the path is
    // entered and accepted again until the dialog closes.
    static void EnterAndAccept(int pid, string title, AutomationElement field, AutomationElement button, string path, string stayed) {
        var fieldWindow = new IntPtr(field.Current.NativeWindowHandle);
        var buttonWindow = new IntPtr(button.Current.NativeWindowHandle);
        DateTime deadline = DateTime.UtcNow.AddSeconds(20);
        while (DateTime.UtcNow < deadline) {
            SendMessage(fieldWindow, SetText, IntPtr.Zero, path);
            Thread.Sleep(300);
            PostMessage(buttonWindow, Click, IntPtr.Zero, IntPtr.Zero);
            for (int wait = 0; wait < 10; wait++) {
                Thread.Sleep(300);
                if (Dialog(pid, title, 0) == null) return;
            }
        }
        throw new Failure(stayed + path);
    }

    static string ValueOf(AutomationElement element) {
        object pattern;
        return element.TryGetCurrentPattern(ValuePattern.Pattern, out pattern) ? ((ValuePattern)pattern).Current.Value : "";
    }

    // A pop-up list's option is chosen as a person chooses it: the list is
    // expanded and the option selected by the name it is announced by.
    static void Select(int pid, AutomationElement list, string option) {
        if (ValueOf(list) == option) return;
        Focus(list);
        object pattern;
        var expand = list.TryGetCurrentPattern(ExpandCollapsePattern.Pattern, out pattern) ? (ExpandCollapsePattern)pattern : null;
        if (expand != null) expand.Expand();
        var named = new AndCondition(new PropertyCondition(AutomationElement.ControlTypeProperty, ControlType.ListItem), new PropertyCondition(AutomationElement.NameProperty, option));
        AutomationElement item = null;
        DateTime deadline = DateTime.UtcNow.AddSeconds(5);
        while (item == null && DateTime.UtcNow < deadline) {
            item = list.FindFirst(TreeScope.Descendants, named);
            if (item == null) {
                foreach (var window in Windows(pid)) {
                    try { item = window.FindFirst(TreeScope.Descendants, named); } catch (ElementNotAvailableException) { }
                    if (item != null) break;
                }
            }
            if (item == null) Thread.Sleep(300);
        }
        if (item == null) throw new Failure("the open list offered no option named '" + option + "'");
        if (!item.TryGetCurrentPattern(SelectionItemPattern.Pattern, out pattern)) throw new Failure("the option '" + option + "' cannot be selected");
        ((SelectionItemPattern)pattern).Select();
        try { if (expand != null) expand.Collapse(); } catch (InvalidOperationException) { }
        deadline = DateTime.UtcNow.AddSeconds(5);
        while (ValueOf(list) != option) {
            if (DateTime.UtcNow > deadline) throw new Failure("the pop-up list holds '" + ValueOf(list) + "'");
            Thread.Sleep(300);
        }
    }

    static object Handle(IDictionary<string, object> request) {
        string op = request.ContainsKey("op") ? (string)request["op"] : "";
        int pid = request.ContainsKey("pid") ? Convert.ToInt32(request["pid"]) : 0;
        var answer = new Dictionary<string, object>();
        switch (op) {
            case "attach": {
                DateTime deadline = DateTime.UtcNow.AddSeconds(60);
                while (Windows(pid).Count == 0) {
                    if (DateTime.UtcNow > deadline) throw new Failure("the application opened no window");
                    Thread.Sleep(500);
                }
                break;
            }
            case "snapshot": answer["nodes"] = Snapshot(pid); break;
            case "press": Press(Element(request)); break;
            case "set": Type(Element(request), (string)request["text"]); break;
            case "choose_folder":
                ChooseFolder(pid, (string)request["title"], (string)request["path"], Convert.ToDouble(request["seconds"]));
                break;
            case "name_new_folder":
                NameNewFolder(pid, (string)request["title"], (string)request["path"], Convert.ToDouble(request["seconds"]));
                break;
            case "select": Select(pid, Element(request), (string)request["option"]); break;
            case "close": {
                object pattern;
                foreach (var window in Windows(pid)) {
                    if (window.Current.ClassName == "wailsWindow" && window.TryGetCurrentPattern(WindowPattern.Pattern, out pattern)) {
                        ((WindowPattern)pattern).Close();
                        answer["how"] = "the window pattern's close";
                        return answer;
                    }
                }
                throw new Failure("the window offers no close");
            }
            default: throw new Failure("unknown request " + op);
        }
        return answer;
    }

    public static void Run() {
        var input = new StreamReader(Console.OpenStandardInput(), new UTF8Encoding(false));
        var output = new StreamWriter(Console.OpenStandardOutput(), new UTF8Encoding(false));
        output.AutoFlush = true;
        string line;
        while ((line = input.ReadLine()) != null) {
            Dictionary<string, object> answer;
            object number = null;
            try {
                var request = (IDictionary<string, object>)Json.DeserializeObject(line);
                if (request.ContainsKey("seq")) number = request["seq"];
                var result = (Dictionary<string, object>)Handle(request);
                result["ok"] = true;
                answer = result;
            } catch (Exception error) {
                answer = new Dictionary<string, object>();
                answer["ok"] = false;
                answer["error"] = error.Message;
            }
            // Every answer carries its request's number, so the caller never
            // takes an answer to a request it gave up on for a later one's.
            answer["seq"] = number;
            output.WriteLine(Json.Serialize(answer));
        }
    }
}
'@
Add-Type -TypeDefinition $source -ReferencedAssemblies UIAutomationClient, UIAutomationTypes, WindowsBase, System.Web.Extensions
[NativeBackend]::Run()
