#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
extern void readmitSnapshotFinished(char *message);

static WKWebView *findWebView(NSView *view) {
 if ([view isKindOfClass:[WKWebView class]]) return (WKWebView *)view;
 for (NSView *child in view.subviews) { WKWebView *found=findWebView(child); if(found) return found; }
 return nil;
}

// Snapshot the actual WKWebView owned by Wails, on its UI thread. This captures
// the app's own content without recording the user's desktop or other windows.
void readmitSnapshot(char *path, double x, double y, double width, double height) {
 NSString *destination=[NSString stringWithUTF8String:path];
 dispatch_async(dispatch_get_main_queue(), ^{
  WKWebView *web=nil;
  for(NSWindow *window in NSApp.windows) { web=findWebView(window.contentView); if(web) break; }
  if(!web) { readmitSnapshotFinished("Wails webview not found"); return; }
  WKSnapshotConfiguration *config=[WKSnapshotConfiguration new];
  config.rect=CGRectMake(x,y,width,height);
  config.snapshotWidth=@(width);
  config.afterScreenUpdates=YES;
  [web takeSnapshotWithConfiguration:config completionHandler:^(NSImage *image,NSError *error) {
   if(error || !image) { readmitSnapshotFinished("WKWebView snapshot failed"); return; }
   NSBitmapImageRep *bitmap=[NSBitmapImageRep imageRepWithData:[image TIFFRepresentation]];
   NSData *png=[bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
   NSError *writeError=nil;
   BOOL ok=[png writeToFile:destination options:NSDataWritingWithoutOverwriting error:&writeError];
   readmitSnapshotFinished(ok ? "" : "PNG could not be written");
  }];
  [config release];
 });
}
