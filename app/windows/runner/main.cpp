#include <flutter/dart_project.h>
#include <flutter/flutter_view_controller.h>
#include <flutter_windows.h>
#include <windows.h>

#include <algorithm>
#include <string>

#include "flutter_window.h"
#include "utils.h"

namespace {

// The window class TigerSB's window is registered with (win32_window.cpp).
constexpr wchar_t kWindowClass[] = L"TigerSBWindow";

// Hands the arguments to the copy of TigerSB that is already open: a
// tigersb:// link it was opened with goes to that window, which comes to the
// front. Returns false when no copy is open.
bool PassToOpenCopy() {
  HWND open = ::FindWindowW(kWindowClass, nullptr);
  if (!open) {
    return false;
  }
  for (const std::string& arg : GetCommandLineArguments()) {
    if (arg.rfind("tigersb:", 0) == 0) {
      COPYDATASTRUCT data{};
      data.dwData = kKeyMessage;
      data.cbData = static_cast<DWORD>(arg.size());
      data.lpData = const_cast<char*>(arg.data());
      ::SendMessageW(open, WM_COPYDATA, 0, reinterpret_cast<LPARAM>(&data));
    }
  }
  if (::IsIconic(open)) {
    ::ShowWindow(open, SW_RESTORE);
  } else {
    ::ShowWindow(open, SW_SHOW);
  }
  ::SetForegroundWindow(open);
  return true;
}

bool HasArgument(const char* name) {
  auto args = GetCommandLineArguments();
  return std::find(args.begin(), args.end(), name) != args.end();
}

}  // namespace

int APIENTRY wWinMain(_In_ HINSTANCE instance, _In_opt_ HINSTANCE prev,
                      _In_ wchar_t *command_line, _In_ int show_command) {
  // One copy of the app at a time, per user.
  HANDLE single = ::CreateMutexW(nullptr, TRUE, L"Local\\TigerSB-app");
  if (::GetLastError() == ERROR_ALREADY_EXISTS) {
    // The open copy may still be starting: give its window a moment.
    for (int i = 0; i < 20; i++) {
      if (PassToOpenCopy()) {
        return EXIT_SUCCESS;
      }
      ::Sleep(100);
    }
    return EXIT_SUCCESS;
  }

  // Attach to console when present (e.g., 'flutter run') or create a
  // new console when running with a debugger.
  if (!::AttachConsole(ATTACH_PARENT_PROCESS) && ::IsDebuggerPresent()) {
    CreateAndAttachConsole();
  }

  // Initialize COM, so that it is available for use in the library and/or
  // plugins.
  ::CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);

  flutter::DartProject project(L"data");

  std::vector<std::string> command_line_arguments =
      GetCommandLineArguments();

  project.set_dart_entrypoint_arguments(std::move(command_line_arguments));

  // Started with Windows and asked to stay out of the way ("Настройки
  // запуска"): the window starts minimized, or never shows.
  FlutterWindow::Start start = FlutterWindow::Start::kOpen;
  if (HasArgument("--closed")) {
    start = FlutterWindow::Start::kHidden;
  } else if (HasArgument("--minimized")) {
    start = FlutterWindow::Start::kMinimized;
  }
  FlutterWindow window(project, start);

  // A phone-shaped window that fits the screen's work area.
  RECT work{};
  ::SystemParametersInfoW(SPI_GETWORKAREA, 0, &work, 0);
  POINT corner{work.left, work.top};
  HMONITOR monitor = ::MonitorFromPoint(corner, MONITOR_DEFAULTTOPRIMARY);
  double scale = FlutterDesktopGetDpiForMonitor(monitor) / 96.0;
  int work_width = static_cast<int>((work.right - work.left) / scale);
  int work_height = static_cast<int>((work.bottom - work.top) / scale);
  int width = 440;
  int height = std::min(860, work_height - 40);
  Win32Window::Point origin(std::max(0, (work_width - width) / 2),
                            std::max(0, (work_height - height) / 2));
  Win32Window::Size size(width, height);
  if (!window.Create(L"TigerSB", origin, size)) {
    return EXIT_FAILURE;
  }
  window.SetQuitOnClose(true);

  ::MSG msg;
  while (::GetMessage(&msg, nullptr, 0, 0)) {
    ::TranslateMessage(&msg);
    ::DispatchMessage(&msg);
  }

  ::CoUninitialize();
  ::ReleaseMutex(single);
  return EXIT_SUCCESS;
}
