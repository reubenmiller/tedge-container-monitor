*** Settings ***
Resource    ./resources/common.robot
Library    Cumulocity
Library    DeviceLibrary    bootstrap_script=bootstrap.sh

Suite Setup    Suite Setup
Test Teardown    Collect Logs
Suite Teardown    Suite Teardown

*** Test Cases ***

Restart container
    Container Command Should be Successful    te/device/main/service/testapp1/cmd/run_container_action/local-1    {"status":"init","action":"restart"}    timeout=30

Stop container
    Container Command Should be Successful    te/device/main/service/testapp1/cmd/run_container_action/local-2    {"status":"init","action":"stop"}

Start container
    Container Command Should be Successful    te/device/main/service/testapp1/cmd/run_container_action/local-3    {"status":"init","action":"start"}

Pause container
    Container Command Should be Successful    te/device/main/service/testapp1/cmd/run_container_action/local-4    {"status":"init","action":"pause"}

Unpause container
    Container Command Should be Successful    te/device/main/service/testapp1/cmd/run_container_action/local-5    {"status":"init","action":"unpause"}


*** Keywords ***

Container Command Should Be Successful
    [Arguments]    ${topic}    ${payload}    ${timeout}=15
    DeviceLibrary.Execute Command    tedge mqtt pub -q 1 -r ${topic} '${payload}'
    ${output}=    DeviceLibrary.Execute Command    timeout ${timeout} tedge mqtt sub ${topic}    ignore_exit_code=${True}
    Should Contain    ${output}    "status":"successful"
    [Teardown]    DeviceLibrary.Execute Command    tedge mqtt pub -r ${topic} ''


Suite Setup
    ${DEVICE_SN}=    Setup
    Set Suite Variable    $DEVICE_SN
    Cumulocity.External Identity Should Exist    ${DEVICE_SN}
    Cumulocity.Should Have Services    name=tedge-container-monitor    service_type=service    min_count=1    max_count=1    timeout=30
    Create Test Container

Create Test Container
    ${operation}=    Cumulocity.Execute Shell Command    sudo tedge-container engine docker network create tedge ||:; sudo tedge-container engine docker run -d --network tedge --name testapp1 httpd:2.4
    Operation Should Be SUCCESSFUL    ${operation}    timeout=60

Remove Test Container
    ${operation}=    Cumulocity.Execute Shell Command    sudo tedge-container engine docker stop --force testapp1 ||:
    Operation Should Be SUCCESSFUL    ${operation}    timeout=60

Suite Teardown
    Remove Test Container

Collect Logs
    Collect Workflow Logs
    Collect Systemd Logs

Collect Systemd Logs
    Execute Command    sudo journalctl -n 10000

Collect Workflow Logs
    Execute Command    cat /var/log/tedge/agent/*
