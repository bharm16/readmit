// Repository-owned vector artwork. Regenerate on macOS:
// swift desktop/packaging/icon.swift /tmp/readmit.iconset
// iconutil -c icns /tmp/readmit.iconset -o desktop/packaging/readmit.icns
import AppKit

let output = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
try FileManager.default.createDirectory(at: output, withIntermediateDirectories: true)
for size in [16, 32, 128, 256, 512] {
    for scale in [1, 2] {
        let pixels = size * scale
        let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: pixels,
            pixelsHigh: pixels, bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true,
            isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)
        let transform = AffineTransform(scale: CGFloat(pixels) / 1024)
        (transform as NSAffineTransform).concat()
        NSColor(red: 0.07, green: 0.23, blue: 0.25, alpha: 1).setFill()
        NSBezierPath(roundedRect: NSRect(x: 64, y: 64, width: 896, height: 896),
                     xRadius: 200, yRadius: 200).fill()
        // A geometric R, drawn as paths so regeneration needs no font.
        let letter = NSBezierPath()
        letter.move(to: NSPoint(x: 306, y: 250))
        letter.line(to: NSPoint(x: 306, y: 780))
        letter.line(to: NSPoint(x: 536, y: 780))
        letter.curve(to: NSPoint(x: 565, y: 465), controlPoint1: NSPoint(x: 787, y: 780),
                     controlPoint2: NSPoint(x: 796, y: 494))
        letter.line(to: NSPoint(x: 750, y: 250))
        letter.line(to: NSPoint(x: 609, y: 250))
        letter.line(to: NSPoint(x: 443, y: 458))
        letter.line(to: NSPoint(x: 420, y: 458))
        letter.line(to: NSPoint(x: 420, y: 250))
        letter.close()
        letter.move(to: NSPoint(x: 420, y: 562))
        letter.line(to: NSPoint(x: 535, y: 562))
        letter.curve(to: NSPoint(x: 535, y: 674), controlPoint1: NSPoint(x: 633, y: 562),
                     controlPoint2: NSPoint(x: 633, y: 674))
        letter.line(to: NSPoint(x: 420, y: 674))
        letter.close()
        letter.windingRule = .evenOdd
        NSColor(red: 0.94, green: 0.97, blue: 0.94, alpha: 1).setFill()
        letter.fill()
        NSGraphicsContext.restoreGraphicsState()
        let suffix = scale == 2 ? "@2x" : ""
        let file = output.appendingPathComponent("icon_\(size)x\(size)\(suffix).png")
        try bitmap.representation(using: .png, properties: [:])!.write(to: file)
    }
}
