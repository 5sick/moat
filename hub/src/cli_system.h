#pragma once

#include <string>
#include <vector>

namespace moat {

// systemd 유닛 내용 (deploy/install-hub.sh와 moat install이 같은 내용을 쓴다)
std::string hubUnitFile();
// `moat-hub install-service`: /etc/systemd/system/moat-hub.service를 쓴다 (daemon-reload는 호출자)
int runInstallService(const std::vector<std::string>& args);
// `moat-hub configure --features JSON | --expose on|off`: 웹 설정과 같은 값을 CLI로 (설치
// 프로그램용)
int runConfigure(const std::vector<std::string>& args);

} // namespace moat
