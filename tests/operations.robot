*** Settings ***
Resource    ./resources/common.robot
Library    Cumulocity
Library    DeviceLibrary    bootstrap_script=bootstrap.sh

Suite Setup    Suite Setup
Test Teardown    Collect Logs

*** Test Cases ***

Get Configuration
    ${binary_url}    Cumulocity.Create Inventory Binary    name    binary_type    file=${CURDIR}/data/tedge-configuration-plugin.toml
    ${operation}=    Cumulocity.Set Configuration    tedge-configuration-plugin    ${binary_url}
    Operation Should Be SUCCESSFUL    ${operation}

    ${operation}=    Cumulocity.Get Configuration    typename=tedge-container-plugin
    Operation Should Be SUCCESSFUL    ${operation}

Install/uninstall container-group package
    ${binary_url}=    Cumulocity.Create Inventory Binary    nginx    container-group    file=${CURDIR}/data/docker-compose.nginx.yaml
    ${operation}=    Cumulocity.Install Software    {"name": "nginx", "version": "1.0.0", "softwareType": "container-group", "url": "${binary_url}"}
    Operation Should Be SUCCESSFUL    ${operation}    timeout=60
    Device Should Have Installed Software    {"name": "nginx", "version": "1.0.0", "softwareType": "container-group"}
    ${operation}=    Cumulocity.Execute Shell Command    wget -O- 127.0.0.1:8080
    Operation Should Be SUCCESSFUL    ${operation}
    Should Contain    ${operation.to_json()["c8y_Command"]["result"]}    Welcome to nginx
    Cumulocity.Should Have Services    name=nginx@nginx    service_type=container-group    status=up

    # Uninstall
    ${operation}=     Cumulocity.Uninstall Software    {"name": "nginx", "version": "1.0.0", "softwareType": "container-group"}
    Operation Should Be SUCCESSFUL    ${operation}
    Device Should Not Have Installed Software    nginx
    Cumulocity.Should Have Services    name=nginx@nginx    service_type=container-group    min_count=0    max_count=0

Install container-group with multiple files
    [Template]    Install container-group file
    app1    1.0.1    app1    ${CURDIR}/data/apps/app1.tar.gz
    app2    1.2.3    app2    ${CURDIR}/data/apps/app2.zip

Install/uninstall container package
    ${operation}=    Cumulocity.Install Software    {"name": "webserver", "version": "httpd:2.4", "softwareType": "container"}
    Operation Should Be SUCCESSFUL    ${operation}    timeout=60
    Device Should Have Installed Software    {"name": "webserver", "version": "httpd:2.4", "softwareType": "container"}
    ${operation}=    Cumulocity.Execute Shell Command    sudo container-cli run --rm -t --network tedge busybox wget -O- webserver:80;
    Operation Should Be SUCCESSFUL    ${operation}
    Should Contain    ${operation.to_json()["c8y_Command"]["result"]}    It works!
    Cumulocity.Should Have Services    name=webserver    service_type=container    status=up

    # Uninstall
    ${operation}=     Cumulocity.Uninstall Software    {"name": "webserver", "version": "httpd:2.4", "softwareType": "container"}
    Operation Should Be SUCCESSFUL    ${operation}
    Device Should Not Have Installed Software    webserver
    Cumulocity.Should Have Services    name=webserver    service_type=container    min_count=0    max_count=0


Install/uninstall container package from file
    ${binary_url}=    Cumulocity.Create Inventory Binary    app3    container    file=${CURDIR}/data/apps/app3.tar

    ${operation}=    Cumulocity.Install Software    {"name": "app3", "version": "app3", "softwareType": "container", "url": "${binary_url}"}
    Operation Should Be SUCCESSFUL    ${operation}
    Device Should Have Installed Software    {"name": "app3", "version": "app3:latest", "softwareType": "container"}
    Cumulocity.Should Have Services    name=app3    service_type=container    status=up

    # Uninstall
    ${operation}=     Cumulocity.Uninstall Software    {"name": "app3", "version": "app3:latest", "softwareType": "container"}
    Operation Should Be SUCCESSFUL    ${operation}
    Device Should Not Have Installed Software    app3
    Cumulocity.Should Have Services    name=app3    service_type=container    min_count=0    max_count=0


Manual container creation/deletion
    ${operation}=    Cumulocity.Execute Shell Command    sudo container-cli network create tedge ||:; sudo container-cli run -d --network tedge --name manualapp1 httpd:2.4
    Operation Should Be SUCCESSFUL    ${operation}    timeout=60

    ${operation}=    Cumulocity.Execute Shell Command    sudo container-cli run --rm -t --network tedge busybox wget -O- manualapp1:80;
    Operation Should Be SUCCESSFUL    ${operation}

    Should Contain    ${operation.to_json()["c8y_Command"]["result"]}    It works!
    Cumulocity.Should Have Services    name=manualapp1    service_type=container    status=up

    # Uninstall
    ${operation}=    Cumulocity.Execute Shell Command    sudo container-cli rm manualapp1 --force
    Operation Should Be SUCCESSFUL    ${operation}
    Cumulocity.Should Have Services    name=manualapp1    service_type=container    min_count=0    max_count=0    timeout=10

*** Keywords ***

Suite Setup
    ${DEVICE_SN}=    Setup
    Set Suite Variable    $DEVICE_SN
    Cumulocity.External Identity Should Exist    ${DEVICE_SN}

    # Create common network for all containers
    ${operation}=    Cumulocity.Execute Shell Command    set -a; . /etc/tedge-container-plugin/env; docker network create tedge ||:

Install container-group file
    [Arguments]    ${package_name}    ${package_version}    ${service_name}    ${file}
    ${binary_url}=    Cumulocity.Create Inventory Binary    ${package_name}    container-group    file=${file}
    ${operation}=    Cumulocity.Install Software    {"name": "${package_name}", "version": "${package_version}", "softwareType": "container-group", "url": "${binary_url}"}
    Operation Should Be SUCCESSFUL    ${operation}    timeout=300
    Device Should Have Installed Software    {"name": "${package_name}", "version": "${package_version}", "softwareType": "container-group"}
    ${operation}=    Cumulocity.Execute Shell Command    sudo container-cli run --rm -t --network tedge busybox wget -O- ${service_name}:80
    Operation Should Be SUCCESSFUL    ${operation}
    Should Contain    ${operation.to_json()["c8y_Command"]["result"]}    My Custom Web Application
    Cumulocity.Should Have Services    name=${package_name}@${service_name}    service_type=container-group    status=up

Collect Logs
    Collect Workflow Logs
    Collect Systemd Logs

Collect Systemd Logs
    Execute Command    sudo journalctl -n 10000

Collect Workflow Logs
    Execute Command    cat /var/log/tedge/agent/*