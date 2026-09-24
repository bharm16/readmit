"""The Linux backend of tools/native_journey.py.

Reads and acts on the installed application through AT-SPI, one JSON request
per line on standard input and one JSON answer per line on standard output.
Every decision about what a journey expects is made by the caller. It runs
under the system Python, whose introspection bindings reach AT-SPI.
"""
import json
import sys
import time

import gi

gi.require_version("Atspi", "2.0")
from gi.repository import Atspi  # noqa: E402

TEXTUAL = {"static", "label", "paragraph", "heading", "section", "list item", "caption", "table cell", "text"}
handles = {}


class Failure(Exception):
    pass


def applications(pid):
    desktop = Atspi.get_desktop(0)
    found = []
    for index in range(desktop.get_child_count()):
        candidate = desktop.get_child_at_index(index)
        try:
            if candidate is not None and candidate.get_process_id() == pid:
                found.append(candidate)
        except gi.repository.GLib.Error:
            continue
    return found


def states(node):
    try:
        return node.get_state_set()
    except gi.repository.GLib.Error:
        return None


def text_of(node):
    return Atspi.Text.get_text(node, 0, Atspi.Text.get_character_count(node))


def own_text(node, role):
    if role not in TEXTUAL:
        return None
    if node.get_text_iface() is None:
        return None
    try:
        # An embedded-object character stands where a child element's own
        # text is read out; the caller puts the child's text there.
        return text_of(node)
    except gi.repository.GLib.Error:
        return None


def snapshot(pid):
    handles.clear()
    nodes = []

    def describe(node, role):
        entry = {"role": role, "name": node.get_name() or ""}
        state = states(node)
        if state is not None:
            entry["enabled"] = state.contains(Atspi.StateType.ENABLED) and state.contains(Atspi.StateType.SENSITIVE)
            entry["focused"] = state.contains(Atspi.StateType.FOCUSED)
            if role == "check box":
                entry["checked"] = state.contains(Atspi.StateType.CHECKED)
        if role in ("entry", "password text") or (role == "text" and node.get_editable_text_iface() is not None):
            if node.get_text_iface() is not None:
                entry["value"] = text_of(node)
        else:
            content = own_text(node, role)
            if content:
                entry["text"] = content
        return entry

    def walk(node, parent, depth):
        if len(nodes) >= 6000 or depth > 90:
            return
        try:
            role = node.get_role_name()
            count = node.get_child_count()
            entry = describe(node, role)
        except Exception:  # noqa: BLE001 - an element that went away is not in the tree
            return
        index = len(nodes)
        handles[index] = node
        entry["id"] = index
        if parent is not None:
            entry["parent"] = parent
        nodes.append(entry)
        for child_index in range(count):
            try:
                child = node.get_child_at_index(child_index)
            except gi.repository.GLib.Error:
                continue
            if child is not None:
                walk(child, index, depth + 1)

    # AT-SPI keeps each application's tree current from its events, so the
    # walk reads that cache rather than asking the application for every
    # element again; only text content is read from the application.
    for application in applications(pid):
        walk(application, None, 0)
    return nodes


def element(request):
    found = handles.get(request.get("id"))
    if found is None:
        raise Failure("no such element in the last snapshot")
    return found


def press(node):
    if node.get_action_iface() is None or Atspi.Action.get_n_actions(node) == 0:
        raise Failure("the element offers no action a person's press performs")
    names = [Atspi.Action.get_action_name(node, index) for index in range(Atspi.Action.get_n_actions(node))]
    for preferred in ("press", "click", "activate", "toggle", "switch"):
        if preferred in names:
            Atspi.Action.do_action(node, names.index(preferred))
            return
    Atspi.Action.do_action(node, 0)


CONTROL_L = 37  # the X keycode of the left Control key in Xvfb's default keymap


def type_into(field, text):
    """Types into a page's field as a person does: a click in it, everything
    it holds selected and deleted, then the text as key presses, so the page
    sees the input events typing makes. With no window manager under Xvfb,
    the click also gives the window the keyboard."""
    # A field below the fold is scrolled into view first, as a person scrolls to it.
    try:
        Atspi.Component.scroll_to(field, Atspi.ScrollType.ANYWHERE)
        time.sleep(0.3)
    except gi.repository.GLib.Error:
        pass
    extents = Atspi.Component.get_extents(field, Atspi.CoordType.SCREEN)
    if extents.width <= 0 or extents.height <= 0:
        raise Failure("the field is not on the screen, so it cannot be typed into")
    x, y = extents.x + min(extents.width // 2, 40), extents.y + extents.height // 2
    Atspi.generate_mouse_event(x, y, "abs")
    Atspi.generate_mouse_event(x, y, "b1c")
    time.sleep(0.2)
    Atspi.generate_keyboard_event(CONTROL_L, None, Atspi.KeySynthType.PRESS)
    Atspi.generate_keyboard_event(0x61, None, Atspi.KeySynthType.SYM)
    Atspi.generate_keyboard_event(CONTROL_L, None, Atspi.KeySynthType.RELEASE)
    Atspi.generate_keyboard_event(0xFFFF, None, Atspi.KeySynthType.SYM)
    if text:
        Atspi.generate_keyboard_event(0, text, Atspi.KeySynthType.STRING)
    time.sleep(0.2)


def dialog(pid, title, seconds):
    deadline = time.monotonic() + seconds
    while True:
        for application in applications(pid):
            for index in range(application.get_child_count()):
                window = application.get_child_at_index(index)
                if window is not None and window.get_name() == title:
                    return window
        if time.monotonic() > deadline:
            return None
        time.sleep(0.3)


def descendants(root):
    out, stack = [], [root]
    while stack and len(out) < 6000:
        node = stack.pop()
        out.append(node)
        try:
            stack.extend(child for child in (node.get_child_at_index(i) for i in range(node.get_child_count())) if child is not None)
        except gi.repository.GLib.Error:
            continue
    return out


def choose_folder(pid, title, path, seconds):
    """GTK's folder chooser is answered as a person answers it: typing a path
    opens its location field, Return goes to that folder, and its accept
    button chooses the folder that shows. With no window manager under Xvfb,
    keys reach the window under the pointer, so the pointer rests on it."""
    chooser = dialog(pid, title, seconds)
    if chooser is None:
        raise Failure(f"no folder dialog titled {title!r} opened")
    extents = Atspi.Component.get_extents(chooser, Atspi.CoordType.SCREEN)
    Atspi.generate_mouse_event(extents.x + extents.width // 2, extents.y + 12, "abs")
    time.sleep(0.3)
    # A typed slash opens the location field holding it; the rest of the
    # path is entered into that field once it shows.
    Atspi.generate_keyboard_event(0, "/", Atspi.KeySynthType.STRING)
    field = None
    deadline = time.monotonic() + 10
    while field is None and time.monotonic() < deadline:
        time.sleep(0.3)
        for node in descendants(chooser):
            try:
                if node.get_role_name() == "text" and node.get_editable_text_iface() is not None \
                        and text_of(node).startswith("/"):
                    field = node
                    break
            except gi.repository.GLib.Error:
                continue
    if field is None:
        describe_chooser(chooser, "after typing a slash")
        raise Failure("a typed slash opened no location field in the folder dialog")
    Atspi.EditableText.set_text_contents(field, path)
    time.sleep(0.5)
    describe_chooser(chooser, "after entering the path")
    if not choose(chooser):
        Atspi.generate_keyboard_event(0xFF0D, None, Atspi.KeySynthType.SYM)
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        time.sleep(0.5)
        if dialog(pid, title, 0) is None:
            return
        describe_chooser(chooser, "still open")
        choose(chooser)
    raise Failure(f"the folder dialog stayed open after choosing {path}")


def choose(chooser):
    """Presses the chooser's accept button if it is offered; says whether it was."""
    accept = next((n for n in descendants(chooser) if n.get_role_name() == "push button" and n.get_name() in ("Open", "Select", "_Open")), None)
    state = states(accept) if accept is not None else None
    if state is None or not state.contains(Atspi.StateType.SENSITIVE):
        return False
    press(accept)
    return True


def describe_chooser(chooser, when):
    """Records what the chooser held, in the backend's log, for a failure."""
    fields = []
    for node in descendants(chooser):
        try:
            role = node.get_role_name()
            if role == "text" and node.get_text_iface() is not None:
                fields.append(("text", text_of(node)))
            elif role == "push button" and node.get_name():
                state = states(node)
                fields.append((node.get_name(), state is not None and state.contains(Atspi.StateType.SENSITIVE)))
        except gi.repository.GLib.Error:
            continue
    print(f"chooser {when}: {fields}", file=sys.stderr, flush=True)


def handle(request):
    op = request.get("op")
    pid = request.get("pid")
    if op == "attach":
        deadline = time.monotonic() + 60
        while not applications(pid):
            if time.monotonic() > deadline:
                raise Failure("the application never appeared on the accessibility bus")
            time.sleep(0.5)
        return {}
    if op == "snapshot":
        return {"nodes": snapshot(pid)}
    if op == "press":
        press(element(request))
        return {}
    if op == "set":
        type_into(element(request), request.get("text", ""))
        return {}
    if op == "choose_folder":
        choose_folder(pid, request["title"], request["path"], request.get("seconds", 60))
        return {}
    if op == "close":
        # Under Xvfb no window manager owns a close button; the caller ends
        # the process, which is what closing the window does to it.
        return {"how": "unsupported"}
    raise Failure(f"unknown request {op}")


def main():
    for line in sys.stdin:
        number = None
        try:
            request = json.loads(line)
            number = request.get("seq")
            answer = handle(request)
            answer["ok"] = True
        except Exception as error:  # noqa: BLE001 - every failure is answered, never a dead backend
            answer = {"ok": False, "error": f"{type(error).__name__}: {error}"}
        # Every answer carries its request's number, so the caller never takes
        # an answer to a request it gave up on for a later one's.
        answer["seq"] = number
        sys.stdout.write(json.dumps(answer) + "\n")
        sys.stdout.flush()


if __name__ == "__main__":
    main()
