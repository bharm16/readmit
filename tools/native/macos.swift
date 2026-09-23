// The macOS backend of tools/native_journey.py: reads and acts on the
// installed application through the Accessibility API, one JSON request per
// line on standard input and one JSON answer per line on standard output.
// Every decision about what a journey expects is made by the caller.
import AppKit
import ApplicationServices
import Foundation

setbuf(stdout, nil)

var handles: [Int: AXUIElement] = [:]

func answer(_ fields: [String: Any]) {
    var body = fields
    if body["ok"] == nil { body["ok"] = true }
    let data = try! JSONSerialization.data(withJSONObject: body)
    FileHandle.standardOutput.write(data)
    FileHandle.standardOutput.write(Data([0x0A]))
}

struct Failure: Error { let message: String }

func attribute(_ element: AXUIElement, _ name: String) -> AnyObject? {
    var value: AnyObject?
    return AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success ? value : nil
}

func string(_ element: AXUIElement, _ name: String) -> String { (attribute(element, name) as? String) ?? "" }

let fetched = [kAXRoleAttribute, kAXSubroleAttribute, kAXTitleAttribute, kAXDescriptionAttribute, kAXValueAttribute,
               kAXEnabledAttribute, kAXFocusedAttribute, kAXChildrenAttribute] as [CFString]

/// The attributes this backend reads, fetched in one round trip.
func attributes(_ element: AXUIElement) -> [String: AnyObject] {
    var values: CFArray?
    guard AXUIElementCopyMultipleAttributeValues(element, fetched as CFArray, AXCopyMultipleAttributeOptions(rawValue: 0), &values) == .success,
          let list = values as? [AnyObject] else { return [:] }
    var out: [String: AnyObject] = [:]
    for (index, key) in fetched.enumerated() where index < list.count {
        let value = list[index]
        // A missing attribute comes back as an AXValue wrapping an error.
        if CFGetTypeID(value) == AXValueGetTypeID(), AXValueGetType(value as! AXValue) == .axError { continue }
        out[key as String] = value
    }
    return out
}

func windows(_ pid: pid_t) -> [AXUIElement] {
    let app = AXUIElementCreateApplication(pid)
    AXUIElementSetMessagingTimeout(app, 10)
    return ((attribute(app, kAXWindowsAttribute) as? [AXUIElement]) ?? []).filter { string($0, kAXRoleAttribute) == kAXWindowRole }
}

func snapshot(_ pid: pid_t) -> [[String: Any]] {
    handles = [:]
    var nodes: [[String: Any]] = []
    var seen: [CFHashCode: [AXUIElement]] = [:]
    func walk(_ element: AXUIElement, _ parent: Int?, _ depth: Int) {
        let hash = CFHash(element)
        if nodes.count >= 6000 || depth > 90 || (seen[hash] ?? []).contains(where: { CFEqual($0, element) }) { return }
        seen[hash, default: []].append(element)
        let values = attributes(element)
        let id = nodes.count
        handles[id] = element
        let role = values[kAXRoleAttribute as String] as? String ?? ""
        let title = values[kAXTitleAttribute as String] as? String ?? ""
        let description = values[kAXDescriptionAttribute as String] as? String ?? ""
        var node: [String: Any] = ["id": id, "role": role, "name": title.isEmpty ? description : title,
                                   "enabled": (values[kAXEnabledAttribute as String] as? Bool) ?? true,
                                   "focused": (values[kAXFocusedAttribute as String] as? Bool) ?? false]
        if let parent { node["parent"] = parent }
        if let subrole = values[kAXSubroleAttribute as String] as? String { node["subrole"] = subrole }
        let value = values[kAXValueAttribute as String]
        if role == kAXStaticTextRole, let text = value as? String {
            node["text"] = text
        } else if let text = value as? String {
            node["value"] = text
        } else if role == kAXCheckBoxRole, let number = value as? NSNumber {
            node["checked"] = number.intValue == 1
        }
        nodes.append(node)
        for child in (values[kAXChildrenAttribute as String] as? [AXUIElement]) ?? [] { walk(child, id, depth + 1) }
    }
    for window in windows(pid) { walk(window, nil, 0) }
    return nodes
}

func element(_ request: [String: Any]) throws -> AXUIElement {
    guard let id = request["id"] as? Int, let found = handles[id] else { throw Failure(message: "no such element in the last snapshot") }
    return found
}

func activate(_ pid: pid_t) {
    NSRunningApplication(processIdentifier: pid)?.activate()
    for _ in 0..<20 where NSWorkspace.shared.frontmostApplication?.processIdentifier != pid {
        Thread.sleep(forTimeInterval: 0.25)
    }
}

/// System Events types for this backend: osascript holds the accessibility
/// grant a hosted runner's image gives it. Keys go only to the application
/// once it is frontmost, never to whatever else has focus.
func keystroke(_ pid: pid_t, _ script: String) throws {
    activate(pid)
    guard NSWorkspace.shared.frontmostApplication?.processIdentifier == pid else {
        throw Failure(message: "the application is not frontmost, so no keys were sent")
    }
    let osascript = Process()
    osascript.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
    osascript.arguments = ["-e", "tell application \"System Events\" to " + script]
    let error = Pipe()
    osascript.standardError = error
    try osascript.run()
    osascript.waitUntilExit()
    if osascript.terminationStatus != 0 {
        let message = String(data: error.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? ""
        throw Failure(message: "System Events refused a keystroke: \(message)")
    }
}

func descendants(_ root: AXUIElement) -> [AXUIElement] {
    var out: [AXUIElement] = []
    var stack = [root]
    while let next = stack.popLast(), out.count < 4000 {
        out.append(next)
        stack.append(contentsOf: (attribute(next, kAXChildrenAttribute) as? [AXUIElement]) ?? [])
    }
    return out
}

func label(_ element: AXUIElement) -> String {
    let title = string(element, kAXTitleAttribute)
    return title.isEmpty ? string(element, kAXDescriptionAttribute) : title
}

/// The folder panel a step opened: a sheet on the window, or a window of its own.
func panel(_ pid: pid_t, _ title: String, timeout: TimeInterval) -> AXUIElement? {
    let deadline = Date().addingTimeInterval(timeout)
    while Date() < deadline {
        for window in windows(pid) {
            if label(window).caseInsensitiveCompare(title) == .orderedSame { return window }
            // A panel shown as a sheet is named by its title, which AppKit
            // may report in lower case; an unnamed sheet counts only alone.
            let sheets = ((attribute(window, kAXChildrenAttribute) as? [AXUIElement]) ?? []).filter { string($0, kAXRoleAttribute) == kAXSheetRole }
            if let named = sheets.first(where: { label($0).caseInsensitiveCompare(title) == .orderedSame }) { return named }
            if sheets.count == 1, label(sheets[0]).isEmpty { return sheets[0] }
        }
        Thread.sleep(forTimeInterval: 0.3)
    }
    return nil
}

func chooseFolder(_ pid: pid_t, _ title: String, _ path: String, _ timeout: TimeInterval) throws {
    guard let open = panel(pid, title, timeout: timeout) else { throw Failure(message: "no folder panel titled \(title.debugDescription) opened") }
    // Go to the folder by its path, as a person types it after Shift-Command-G.
    try keystroke(pid, "keystroke \"g\" using {command down, shift down}")
    var field: AXUIElement?
    let deadline = Date().addingTimeInterval(10)
    while field == nil && Date() < deadline {
        field = descendants(open).first { element in
            let role = string(element, kAXRoleAttribute)
            return (role == kAXTextFieldRole || role == kAXComboBoxRole) && string(element, kAXSubroleAttribute) != kAXSearchFieldSubrole
        }
        if field == nil { Thread.sleep(forTimeInterval: 0.3) }
    }
    guard let field else { throw Failure(message: "the panel offered no field to type the folder into") }
    AXUIElementSetAttributeValue(field, kAXFocusedAttribute as CFString, kCFBooleanTrue)
    try keystroke(pid, "keystroke \"a\" using {command down}")
    let escaped = path.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"")
    try keystroke(pid, "keystroke \"\(escaped)\"")
    Thread.sleep(forTimeInterval: 0.5)
    try keystroke(pid, "key code 36")
    Thread.sleep(forTimeInterval: 1)
    // The panel now shows that folder; its default button chooses it.
    let buttons = descendants(open).filter { string($0, kAXRoleAttribute) == kAXButtonRole && ["Open", "Choose", "Select"].contains(label($0)) }
    guard let accept = buttons.first else { throw Failure(message: "the panel offered no button to choose the folder") }
    AXUIElementPerformAction(accept, kAXPressAction as CFString)
    let closed = Date().addingTimeInterval(10)
    while Date() < closed {
        if panel(pid, title, timeout: 0.1) == nil { return }
        Thread.sleep(forTimeInterval: 0.3)
    }
    throw Failure(message: "the folder panel stayed open after choosing \(path)")
}

func handle(_ request: [String: Any]) throws -> [String: Any] {
    let op = request["op"] as? String ?? ""
    let pid = pid_t(request["pid"] as? Int ?? 0)
    switch op {
    case "attach":
        guard AXIsProcessTrusted() else { throw Failure(message: "this process holds no accessibility permission") }
        let deadline = Date().addingTimeInterval(60)
        while windows(pid).isEmpty {
            if Date() > deadline { throw Failure(message: "the application opened no window") }
            Thread.sleep(forTimeInterval: 0.5)
        }
        return [:]
    case "snapshot":
        return ["nodes": snapshot(pid)]
    case "press":
        let target = try element(request)
        let result = AXUIElementPerformAction(target, kAXPressAction as CFString)
        if result != .success { throw Failure(message: "press failed: \(result.rawValue)") }
        return [:]
    case "set":
        let target = try element(request)
        let text = request["text"] as? String ?? ""
        AXUIElementSetAttributeValue(target, kAXFocusedAttribute as CFString, kCFBooleanTrue)
        let result = AXUIElementSetAttributeValue(target, kAXValueAttribute as CFString, text as CFString)
        if result != .success { throw Failure(message: "setting the value failed: \(result.rawValue)") }
        return [:]
    case "choose_folder":
        try chooseFolder(pid, request["title"] as? String ?? "", request["path"] as? String ?? "", TimeInterval(request["seconds"] as? Int ?? 60))
        return [:]
    case "close":
        guard let window = windows(pid).first, let button = attribute(window, kAXCloseButtonAttribute) else {
            throw Failure(message: "the window offers no close button")
        }
        AXUIElementPerformAction(button as! AXUIElement, kAXPressAction as CFString)
        return ["how": "the window's close button"]
    default:
        throw Failure(message: "unknown request \(op)")
    }
}

while let line = readLine() {
    // Every answer carries its request's number, so the caller never takes an
    // answer to a request it gave up on for the answer to a later one.
    let request = (try? JSONSerialization.jsonObject(with: Data(line.utf8))) as? [String: Any]
    let number: Any = request?["seq"] ?? NSNull()
    do {
        guard let request else { throw Failure(message: "a request is a JSON object") }
        var result = try handle(request)
        result["seq"] = number
        answer(result)
    } catch let failure as Failure {
        answer(["ok": false, "error": failure.message, "seq": number])
    } catch {
        answer(["ok": false, "error": "\(error)", "seq": number])
    }
}
