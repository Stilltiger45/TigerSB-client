#ifndef RUNNER_FLUTTER_WINDOW_H_
#define RUNNER_FLUTTER_WINDOW_H_

#include <flutter/dart_project.h>
#include <flutter/encodable_value.h>
#include <flutter/flutter_view_controller.h>
#include <flutter/method_channel.h>

#include <memory>

#include "win32_window.h"

// WM_COPYDATA's dwData for a tigersb:// link from another copy of the app.
constexpr ULONG_PTR kKeyMessage = 0x54534B31;  // "TSK1"

// A window that hosts the Flutter view and passes on the links that other
// copies of the app hand it.
class FlutterWindow : public Win32Window {
 public:
  // How the window first appears: as usual, minimized, or not at all (the
  // app started with Windows «закрытым» turns the VPN on and quits).
  enum class Start { kOpen, kMinimized, kHidden };

  // Creates a new FlutterWindow hosting a Flutter view running |project|.
  FlutterWindow(const flutter::DartProject& project, Start start);
  virtual ~FlutterWindow();

 protected:
  // Win32Window:
  bool OnCreate() override;
  void OnDestroy() override;
  LRESULT MessageHandler(HWND window, UINT const message, WPARAM const wparam,
                         LPARAM const lparam) noexcept override;

 private:
  // The project to run.
  flutter::DartProject project_;

  Start start_;

  // The Flutter instance hosted by this window.
  std::unique_ptr<flutter::FlutterViewController> flutter_controller_;

  // "tigersb/windows": the links from other copies go to Dart here.
  std::unique_ptr<flutter::MethodChannel<flutter::EncodableValue>> channel_;
};

#endif  // RUNNER_FLUTTER_WINDOW_H_
