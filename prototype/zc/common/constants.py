#!/usr/bin/env python3
# -*- coding: utf-8 -*-
'''
Zabbix Cloning: Zabbix monitoring settings cloning tool, from master-Zabbix to worker-Zabbix.

Copyright (c) 2024 tsuno teppei
Released under the MIT license
https://opensource.org/licenses/mit-license.php
'''
__author__  = 'tsuno.teppei'
__version__ = '0.1.10'
__date__    = '2026/02/19'

import os
import sys
import json
import uuid
from zabbix_utils import ZabbixAPI
import re
import bz2
import socket
from datetime import datetime, UTC
from calendar import timegm
from time import sleep
from concurrent import futures
import inspect
import argparse
import shutil
import textwrap
import logging

# ZABBIX関連の固定値とか
ZABBIX_DEFAULT_AUTH = {'user': 'Admin', 'password': 'zabbix'}
ZABBIX_CONFIG_PATH = '/etc/zabbix'
ZABBIX_SERVER_CONFIG = 'zabbix_server.conf'
ZABBIX_USER_CONFIG_PATH = '/var/lib/zabbix/conf.d'
ZABBIX_SNMP_COMMUNITY = '{$SNMP_COMMUNITY}'
ZABBIX_TEMPLATE_ROOT = 'Templates'
ZABBIX_ENABLE = '0'
ZABBIX_DISABLE = '1'
ZABBIX_SUPER_USER = 'Admin'
ZABBIX_SUPER_GROUP = 'Zabbix administrators'
ZABBIX_SUPER_ROLE = 3
ZABBIX_WEEKDAY = {'MON': 1, 'TUE': 2, 'WED': 3, 'THU': 4, 'FRI': 5, 'SAT': 6, 'SUN': 7}
ZABBIX_INVENTORY_MODE = {'DISABLED': -1, 'MANUAL': 0, 'AOTOMATIC': 1}
ZABBIX_IFTYPE = {'AGENT': 1, 'SNMP': 2, 'IPMI': 3, 'JMX': 4, 1: 'AGENT', 2: 'SNMP', 3: 'IPMI', 4: 'JMX'}
ZABBIX_SNMP_VERSION = {'SNMPV1': 1, 'SNMPV2': 2, 'SNMPV3': 3}
ZABBIX_PROXY_MODE = {'direct': 0, 'proxy': 1, 'proxy_group': 2}

# 並行処理同時実行数のデフォルト値
PHP_WORKER_NUM = 4

# ZabbixCloneパラメーター
ZC_DEFAULT_ZABBIX_VERSION = 7.0
ZC_SUPPORT_VERSION_LOWER = 4.0
ZC_DEFAULT_NODE = 'zabbix'
ZC_DERAULT_ROLE = 'master'
ZC_HEAD = 'ZC_'
ZC_UNIQUE_TAG = ZC_HEAD + 'UUID'
ZC_CONFIG = 'zc.conf'
ZC_MAINTE_NAME = '__ZC_UPDATE__'
ZC_MONITOR_TAG = ZC_HEAD + 'WORKER'
ZC_NOTICE = 'Email'
ZC_NOTICE_USER = ZABBIX_SUPER_USER
ZC_NOTICE_TO = 'alert@example.com'
ZC_DEFAULT_STORE = 'file'
ZC_NO_NOTICE_ROLE = ['replica']
ZC_COMPLETE = (True, 'Complete.')
ZC_TEMPLATE_SEPARATE = 100
ZC_NODE_ID = 'ZC_NODE_ID'
ZC_FILE_STORE = ['/var/lib/zabbix', 'Documents']
ZC_VERSION_CODE = '{$ZC_VERSION}'

# 表示系
SIZE = shutil.get_terminal_size()
WIDE_COUNT = SIZE.columns
LINE_COUNT = SIZE.lines
T_CHAR = ' '
T_COUNT = 2
TAB = T_CHAR * T_COUNT
B_CHAR = '-'
B_COUNT = WIDE_COUNT - T_COUNT
BD = B_CHAR * B_COUNT

# 6.0以降Settingsデフォルト値
## 7.0 のタイムアウト対応、ExternalCheckがタイムアウトするとZabbixが死ぬので注意（7.0.2で確認）
ZC_TIMEOUT_LOWER = {
    'external_check': 15,
}

# アラート通知デフォルト
ZC_DEFAILT_ALERT = {
    'user': [
        [ZC_NOTICE_USER, ZC_NOTICE_TO]
    ],
    'severity': {i: True for i in range(6)},
    'worktime': {day: '00:00-24:00' for day in ZABBIX_WEEKDAY.keys()}
}

DEFAULT_LOG_LEVEL = 'INFO'
DEFAULT_LOG_DATE = '%Y-%m-%d %H:%M:%S'
DEFAULT_LOG_FORMAT = '%(asctime)s.%(msecs)03d %(name)s %(funcName)s.%(lineno)s [%(levelname)s]: %(message)s'
DEFAULT_LOG_STREAM = {
    'handler': 'StreamHandler',
    'format': '%(message)s'
}
DEFAULT_LOG_FILE = {
    'handler': 'FileHandler',
    'format': DEFAULT_LOG_FORMAT,
    'option': {
        'filename': os.path.join(
            os.environ.get('userprofile'),
            ZC_FILE_STORE[1],
            'zc',
            'log',
            'zc.log'
        ) if os.name == 'nt' else os.path.join(
            ZC_FILE_STORE[0],
            'zc',
            'log',
            'zc.log'
        )
    }
}
DEFAULT_LOG = {
    'logName': __name__,
    'logLevel': DEFAULT_LOG_LEVEL,
    'logHandlers': [DEFAULT_LOG_STREAM]
}
