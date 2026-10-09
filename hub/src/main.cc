#include "app.h"
#include "cli_i18n.h"
#include "cli_invite.h"
#include "cli_recovery.h"
#include "cli_service.h"
#include "cli_system.h"
#include "cli_token.h"
#include "init.h"
#include "net/https_client.h"
#include "options.h"
#include "version.h"

#include <drogon/drogon.h>

#include <cstdio>
#include <filesystem>
#include <iostream>

int main(int argc, char* argv[]) {
    // journald(파이프)로 보낼 때도 로그가 즉시 보이도록 줄 단위 버퍼링
    std::setvbuf(stdout, nullptr, _IOLBF, 0);
    if (argc > 1 && std::string(argv[1]) == "init")
        return moat::runInit({argv + 2, argv + argc});
    if (argc > 1 && std::string(argv[1]) == "join-token")
        return moat::runJoinToken({argv + 2, argv + argc});
    if (argc > 1 && std::string(argv[1]) == "invite")
        return moat::runInvite({argv + 2, argv + argc});
    if (argc > 1 && std::string(argv[1]) == "install-service")
        return moat::runInstallService({argv + 2, argv + argc});
    if (argc > 1 && std::string(argv[1]) == "configure")
        return moat::runConfigure({argv + 2, argv + argc});
    if (argc > 1 && std::string(argv[1]) == "node-set")
        return moat::runNodeSet({argv + 2, argv + argc});
    if (argc > 1 && std::string(argv[1]) == "service-add")
        return moat::runServiceAdd({argv + 2, argv + argc});
    if (argc > 1 && std::string(argv[1]) == "recovery-codes")
        return moat::runRecoveryCodes({argv + 2, argv + argc});
    if (argc > 1 && std::string(argv[1]) == "backup")
        return moat::runBackup({argv + 2, argv + argc});
    if (argc > 1 && std::string(argv[1]) == "restore")
        return moat::runRestore({argv + 2, argv + argc});
    std::string error;
    auto opts = moat::parseOptions({argv + 1, argv + argc}, error);
    if (!opts) {
        std::cerr << error << "\n" << moat::T(moat::usage());
        return 2;
    }
    if (opts->showHelp) {
        std::cout << moat::T(moat::usage());
        return 0;
    }
    if (opts->showVersion) {
        std::cout << "moat-hub " << moat::kVersion << "\n";
        return 0;
    }
    if (opts->configPath.empty()) {
        std::cerr << moat::T("--config 가 필요합니다\n") << moat::T(moat::usage());
        return 2;
    }
    auto cfg = moat::loadConfigFile(opts->configPath, error);
    if (!cfg) {
        std::cerr << error << "\n";
        return 2;
    }
    if (opts->listenSet) {
        cfg->listenAddress = opts->listenAddress;
        cfg->listenPort = opts->listenPort;
    }

    moat::httpGlobalInit();
    if (!moat::httpsSupported()) {
        std::cerr << moat::T("libcurl에 TLS 지원이 없습니다 (Google 로그인 불가)\n");
        return 1;
    }
    std::unique_ptr<moat::HubApp> hub;
    try {
        hub = std::make_unique<moat::HubApp>(*cfg);
    } catch (const std::exception& e) {
        std::cerr << moat::T("초기화 실패: ") << e.what() << "\n";
        return 1;
    }
    hub->registerRoutes();
    hub->startBackgroundTasks();

    LOG_INFO << "moat-hub " << moat::kVersion << " listening on " << cfg->listenAddress << ":"
             << cfg->listenPort << " (public " << cfg->publicUrl << ")";
    drogon::app()
        .addListener(cfg->listenAddress, cfg->listenPort)
        .setThreadNum(2)
        .setServerHeaderField("moat")
        .setClientMaxWebSocketMessageSize(1024 * 1024)
        // 업로드 기능은 쓰지 않지만 Drogon이 시작 시 폴더를 만든다 → 쓰기 가능한 /tmp 아래로
        // (systemd PrivateTmp)
        .setUploadPath((std::filesystem::temp_directory_path() / "moat-uploads").string())
        .run();
    return 0;
}
