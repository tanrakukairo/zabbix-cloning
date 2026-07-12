#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common.constants import *

# 実行時のunixtime生成（UTC）
def UNIXTIME():
    return int(timegm(datetime.now(UTC).timetuple()))

# 実行時のunixtimeをZABBIXの時刻フォーマットに変換（UTC）
def ZABBIX_TIME():
    return datetime.now(UTC).strftime('%Y-%m-%dT%H:%M:%SZ')

# リスト１のすべてがリスト２にあるか確認
def LISTA_ALL_IN_LISTB(listA=[], listB=[]):
    if not isinstance(listA, list) or not isinstance(listB, list):
        return False
    return all(map(listB.__contains__, listA))

def PRINT_PROG(value, quiet=False):
    if not quiet:
        print(value, end='', flush=True)
    return

def PRINT_TAB(num, quiet=False):
    if not quiet:
        print(TAB*num, end='', flush=True)
    return

# ノード名の確認
def CHECK_ZABBIX_SERVER_NAME(endpoint, name, auth):
    '''
    Zabbix公式がapiinfo.servername()実装したらそっち使うので
    そのときimport requestsまとめて消すためにこっちに入れておく
    '''
    import requests

    prefix = '<div class="server-name">'
    suffix = '</div>'
    if auth.get('token'):
        # トークン認証が設定されている場合はBearerトークンでアクセスする
        headers = {'Authorization': f'Bearer {auth["token"]}'}
    else:
        # トークン認証が設定されていない場合はBasic認証でアクセスする
        auth = (auth['user'], auth['password'])
        headers = {'Authorization': f'Basic {requests.auth._basic_auth_str(*auth)}'}
    # Zabbixのデフォルトログインフォームを表示
    endpoint = endpoint.rstrip('/') + '/index.php?form=default'
    res = requests.get(endpoint, headers=headers)
    if not res.ok:
        return (False, 'Cannot Get ServerName.')
    res = re.findall(f'{prefix}[a-zA-Z0-9-]*{suffix}', res.text)
    if not res:
        return (False, 'Not Find ServerName.')
    res = res[0].replace(prefix, '')
    res = res.replace(suffix, '')
    if res != name:
        return (False, f'Wrong Target Node {name}.')
    return ZC_COMPLETE

