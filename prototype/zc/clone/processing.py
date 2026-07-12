#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common.constants import *
from zc.common.utils import *

class CloneProcessingMixin:
    '''
    Per-method data conversion processing for clone operations.
    '''

    # Dispatcher

    def processingMethodData(self, section=''):
        '''
        self.STORE上のsections['POST']のID変換対象のメソッドのデータをIDからNAMEに変換する
        '''
        result = ZC_COMPLETE
        res = []

        if not self.sections.get(section):
            return (False, f'No section:{section} in sections.')
        methods = self.sections[section]

        for method in methods:
            function = 'processing' + method[0].upper() + method[1:]
            if function in self.__dir__():
                result = getattr(self, function)()
                if result == ZC_COMPLETE:
                    rWord = 'Done.'
                elif result[0]:
                    rWord = result[1]
                else:
                    rWord = 'Failed.'
            else:
                rWord = 'None Processing.'
            if not result[0]:
                break
            else:
                res.append(f'[{method}]: {rWord}')

        return (True, res) if result[0] else result

    # Basic and alert-related data

    def processingRegexp(self):
        '''
        regexpの加工
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('regexp'):
            return (True, 'No Exist Data.')
        
        for item in self.STORE['regexp']:
            data = item['DATA']
            if self.isMaster:
                pass
            else:
                for expression in data['expressions']:
                    if int(expression['expression_type']) != 1:
                        # これを使用する１以外ではエラーになるので削除
                        expression.pop('exp_delimiter', None)
        
        return result

    def processingAction(self):
        '''
        ActionのID加工
        マスターノードはローカルデータを加工、ワーカーノードはストアデータを加工
        actionid/operationid/op*idは削除、他idは名称に置換
        userid/groupid/usrgrpid はid2nameでnameに変換
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('action'):
            return (True, 'No Exist Data.')

        # createに不要なパラメータ―
        readOnly = self.discardParameter['action']
        discardOperate = ['esc_period', 'esc_step_from', 'esc_step_to']
        discardNotTriggerAction = ['pause_symptoms', 'pause_suppressed', 'notify_if_canceled']


        items = []
        updateAction = False
        try:
            for item in self.STORE['action'].copy():
                data = item['DATA']
                if self.isMaster:
                    # マスター
                    pass
                else:
                    if self.isReplica:
                        # レプリカ処理
                        pass
                    else:
                        # ワーカー処理
                        if data['status'] == ZABBIX_DISABLE:
                            # 有効でないアクションは除外
                            continue
                    if self.CONFIG.disableMonitoring:
                        # 監視停止設定の場合、全てのアクションを無効化
                        data['status'] = ZABBIX_DISABLE
                    # updateで不要なeventsource削除のためのフラグ
                    if item['NAME'] in self.LOCAL['action'].keys():
                        updateAction = True
                    else:
                        updateAction = False
                # キー名ゆれ対応
                operateType = ['operations', 'recoveryOperations', 'acknowledgeOperations']
                for target in operateType.copy():
                    if target != 'operations':
                        targetData = data.pop(target, None)
                        # get/create間表記ゆれ対応（O -> _o）
                        rename = target.replace('O', '_o')
                        if not targetData:
                            targetData = data.pop(rename, None)
                        # 6.0対応
                        if self.VERSION.major >= 6.0:
                            rename = rename.replace('acknowledge', 'update')
                        if not targetData:
                            targetData = data.pop(target, None)
                        if targetData:
                            data[rename] = targetData
                        # 入れ替え
                        operateType.remove(target)  
                        operateType.append(rename)

                eventSource = int(data['eventsource'])
                if updateAction:
                    data.pop('eventsource', None)
                # トリガーアクション以外で不要なものの削除
                if eventSource != 0:
                    [data.pop(param, None) for param in discardNotTriggerAction]
                # アップデートはトリガー/サービスアクションでのみ使用
                if eventSource in [1, 2, 3]:
                    data.pop('update_operations', None)
                    data.pop('updateOperations', None)
                    data.pop('acknowledge_operations', None)
                    data.pop('acknowledgeOperations', None)
                # ネットワークディスカバリと自動登録で不要なものの削除
                if eventSource in [1, 2]:
                    data.pop('recovery_operations', None)
                    data.pop('recoveryOperations', None)
                    data.pop('esc_period', None)

                # ZABBIXが動的に付けるのでeval_formulaを削除する
                data['filter'].pop('eval_formula', None)
                # 計算式を自動にしているならformulaを削除
                if int(data['filter'].get('evaltype', 0)) < 3:
                    data['filter'].pop('formula', None)
                    custom_formula = False
                else:
                    # カスタム計算式判定を利用
                    custom_formula = True
                # アクション条件のID変換処理
                for filter_item in data['filter']['conditions']:
                    # 6.0以降で入っているとエラーになる項目を削除
                    if self.VERSION.major >= 6.0:
                        if not custom_formula:
                            filter_item.pop('formulaid', None)
                        if not filter_item.get('value'):
                            filter_item.pop('value', None)
                        if not filter_item.get('value2'):
                            filter_item.pop('value2', None)
                    # ID変換対象メソッドを決定
                    condType = int(filter_item['conditiontype'])
                    if condType == 0:
                        method = 'hostgroup'
                    elif condType == 1:
                        method = 'host'
                    elif condType == 13:
                        method = 'template'
                    else:
                        # 対応していない要素
                        # filter_item['conditiontype'] == '2':
                        # Trigger直指定はNode間で同定が難しいので非対応
                        continue
                    # ID変換を実行
                    filter_item.update(
                        {
                            'value': self.replaceIdName(method, filter_item['value'])
                        }
                    )

                # 変換
                for target in operateType:
                    if not data.get(target):
                        data.pop(target, None)
                        continue
                    for operate in data[target]:
                        # 不要データの削除
                        # 空データの削除
                        [operate.pop(param, None) for param in operate.copy().keys() if not operate.get(param)]
                        # ZABBIXが自動的に付けるIDを削除
                        [operate.pop(param, None) for param in readOnly]
                        # トリガーアクション以外では不要なものの削除
                        if eventSource != 0:
                            operate.pop('evaltype', None)
                        # ネットワークディスカバリと自動登録で不要なものの削除
                        if eventSource in [1, 2]:
                            [operate.pop(param, None) for param in discardOperate]
                        # 更新と復帰の処理
                        if target != 'operations':
                            # 6.0以前でここにある条件式は削除
                            operate.pop('evaltype', None)
                            # 全メディア通知の場合、メッセージ設定されている場合はメディアIDを削除
                            if int(operate.get('operationtype')) == 11:
                                operate['opmessage'].pop('mediatypeid', None)
                        # オペレーション内容の処理
                        for op in operate.copy().keys():
                            opData = operate.get(op)
                            if not opData:
                                # アクション実行内容がないものは削除
                                operate.pop(op)
                                continue
                            if isinstance(opData, dict):
                                # 辞書型データの処理
                                # 実行内容がないものを削除
                                [opData.pop(param, None) for param in opData.copy().keys() if not opData.get(param)]
                                # 削除対象
                                [opData.pop(param, None) for param in readOnly]
                                # ID変換
                                for param in opData.copy().keys():
                                    method = self.getMethodFromIdname(param)
                                    if not method:
                                        continue
                                    trans = self.replaceIdName(method, opData[param])
                                    if trans is None:
                                        continue
                                    opData[param] = trans
                            elif isinstance(opData, list):
                                # リスト型データの処理
                                transData = []
                                for opd in opData:
                                    if not isinstance(opd, dict):
                                        # 要素は全部Dictのはず
                                        continue
                                    for param in opd.keys():
                                        # 削除対象
                                        if param in readOnly:
                                            continue
                                        # ID変換
                                        method = self.getMethodFromIdname(param)
                                        trans = self.replaceIdName(method, opd[param])
                                        if trans is None:
                                            continue
                                        transData.append({param: trans})
                                opData = transData
                            else:
                                # dictでもlistでもないのは処理しない
                                pass
                            if not opData:
                                # 空になったものは捨てる
                                operate.pop(op, None)
                            operate[op] = opData
                items.append(item)
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, 'Failed processingAction.')
        self.STORE['action'] = items
        return result

    def processingMediatype(self):
        '''
        MediatypeのID変換
        4.4で不要になるけど4.0/4.2で使う
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('mediatype'):
            return (True, 'No Exist Data.')
        items = []
        try:
            # 処理はあとで
            pass
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, 'Failed processingMediatype.')
        self.STORE['mediatype'] = items
        return result

    def processingScript(self):
        '''
        ScriptのID変換
        マスターノードはローカルデータを加工、ワーカーノードはストアデータを加工
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('script'):
            return (True, 'No Exist Data.')

        items =[]
        try:
            for item in self.STORE['script'].copy():
                data = item['DATA']
                # 共通処理
                # ID変換
                for method in ['usergroup', 'hostgroup']:
                    idName = self.getKeynameInMethod(method, 'id')
                    if data.get(idName):
                        data[idName] = self.replaceIdName(method, data[idName])

                if self.isMaster:
                    # マスターノード処理
                    pass
                else:
                    # ワーカーノード処理
                    scriptType = int(data['type'])
                    scope = int(data.get('scope', 0))
                    # 5.4対応
                    if self.VERSION.major >= 5.4:
                        # Webhook script用パラメーターの削除
                        if scriptType != 0:
                            # Scriptではない
                            data.pop('execute_on', None)
                        if scriptType != 2:
                            # SSHではない
                            data.pop('authtype', None)
                            data.pop('publickey', None)
                            data.pop('privatekey', None)
                            if scriptType != 3:
                                # Telnetでもない
                                data.pop('username', None)
                                data.pop('password', None)
                                data.pop('port', None)
                        else:
                            # SSH/Telnetである
                            if int(data['authtype']) == 0:
                                # パスワード認証である
                                data.pop('publickey', None)
                                data.pop('privatekey', None)
                            else:
                                # 鍵認証である
                                data.pop('password', None)
                        if scriptType != 5:
                            # Wehhooではない
                            data.pop('timeout', None)
                            data.pop('parameters', None)
                        if scope not in [2, 4]:
                            # スコープがmanual host action/manual event actionではない
                            data.pop('menu_path', None)
                            data.pop('usrgrpid', None)
                            data.pop('host_access', None)
                            data.pop('confirmation', None)
                    # 6.4対応
                    if self.VERSION.major >= 6.4:
                        # URL用パラメーターの削除
                        if scriptType != 6:
                            data.pop('url', None)
                            data.pop('new_window', None)
                    # 7.0 対応
                    if self.VERSION.major >= 7.0:
                        # スコープがmanual host action/manual event actionではない
                        # またはmanualinputが0である
                        if scope not in [2, 4] or int(data.get('manualinput', 0)) == 0:
                            data.pop('manualinput', None)
                            data.pop('manualinput_prompt', None)
                            data.pop('manualinput_validator', None)
                            data.pop('manualinput_validator_type', None)
                            data.pop('manualinput_default_value', None)
                        else:
                            if int(data.get('manualinput_validator_type', 0)) == 1:
                                data.pop('manualinput_default_value', None)
                items.append(item)
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, 'Failed processingScript')
        self.STORE['script'] = items
        return result

    # Operation scheduling and network discovery

    def processingMaintenance(self):
        '''
        Maintenanceのデータ加工
        マスターノードはローカルデータを加工、ワーカーノードはストアデータを加工
        maintenanceメソッドはcreateとgetで対象リストのキー名が違うので、マスター側で加工する
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('maintenance'):
            return (True, 'No Exist Data.')

        items = []
        try:
            for item in self.STORE['maintenance'].copy():
                data = item['DATA']
                # 一回限りのメンテの期限切れを削除
                for period in data['timeperiods'].copy():
                    if int(period['timeperiod_type']) == 0:
                        if int(period['start_date']) + int(period['period']) < UNIXTIME():
                            data['timeperiods'].remove(period)
                        # 一回限りのメンテナンスで不要なものの削除
                        period.pop('start_time', None)
                        period.pop('every', None)
                        period.pop('day', None)
                        period.pop('dayofweek', None)
                        period.pop('month', None)
                    elif int(period['timeperiod_type']) == 1:
                        # 毎日に不要なものの削除
                        period.pop('start_date', None)
                        period.pop('dayofweek', None)
                    elif int(period['timeperiod_type']) == 2:
                        # 毎週に不要なものの削除
                        period.pop('start_date', None)
                        period.pop('day', None)
                    elif int(period['timeperiod_type']) == 3:
                        # 毎月に不要なものの削除
                        period.pop('start_date', None)
                    else:
                        pass
                # メンテ期間が空またはメンテウィンドウの終了が現在より後（期限切れ）を削除
                if not data['timeperiods']:
                    continue
                if int(data['active_till']) < UNIXTIME():
                    continue
                if self.isMaster:
                    # 6.2対応
                    if self.VERSION.major >= 6.2:
                        hosts = 'hosts'
                        groups = 'hostgroups'
                    else:
                        hosts = 'hosts'
                        groups = 'groups'
                    if not data.get(groups) and not data.get(hosts):
                        # グループもホストも空の場合はスキップ
                        continue
                    # マスターノード側処理: 対象リストをIDのみに変換
                    # ホストグループリスト
                    name = self.getKeynameInMethod('hostgroup', 'name')
                    data[groups] = [target[name] for target in data.get(groups, [])]
                    if not data[groups]:
                        data.pop(groups)
                    # ホストリスト
                    name = self.getKeynameInMethod('host', 'name')
                    data[hosts] = [target[name] for target in data.pop(hosts, [])]
                    if not data[hosts]:
                        data.pop(hosts)
                    if not data['tags']:
                        data.pop('tags')
                else:
                    if self.VERSION.major >= 6.2:
                        hosts = 'hosts'
                        groups = 'groups'
                    else:
                        hosts = 'hostids'
                        groups = 'groupids'
                    # データ側のバージョンでの変更
                    if self.getLatestVersion('MASTER_VERSION') >= 6.2:
                        storeIds = {
                            hosts: 'hosts',
                            groups: 'hostgroups'
                        }
                    else:
                        storeIds = {
                            hosts: 'hosts',
                            groups: 'groups'
                        }
                    if not data.get(groups) and not data.get(hosts):
                        # グループもホストも空の場合はスキップ
                        continue
                    # ワーカーノード側処理: 対象リストの中を{idName: id}に変換
                    for section in [groups, hosts]:
                        targets = data.pop(storeIds[section], [])
                        method = 'host' if section == hosts else 'hostgroup'
                        id = self.getKeynameInMethod(method, 'id')
                        if targets:
                            data[section] = [
                                {
                                    id: self.replaceIdName(method, target)
                                } for target in targets if self.replaceIdName(method, target)
                            ]
                items.append(item)
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, 'Failed processingMaintenance.')
        self.STORE['maintenance'] = items
        return result

    def processingProxy(self):
        '''
        proxyのデータ加工
        バージョン共通: psk利用時のid/pskの代入
        >=7.0: プロキシグループのID変換
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('proxy'):
            return (True, 'No Exist Data.')

        items = []
        deleteTarget = []
        try:
            for item in self.STORE['proxy'].copy():
                data = item['DATA']
                if self.isMaster:
                    # 7.0対応
                    if self.VERSION.major >= 7.0:
                        # プロキシグループのID変換
                        id = self.getKeynameInMethod('proxygroup', 'id')
                        data[id] = self.replaceIdName('proxygroup', data[id])
                else: 
                    # ワーカーノード処理
                    # 不要データを削除
                    for param in self.discardParameter['proxy']:
                        data.pop(param, None)
                    # 7.0系timeout系は上書きなしまたは空だったら削除
                    for timeout in [param for param in data if re.match('timeout_', param)]:
                        if int(data.get('custom_timeouts', 0)) == 0 or not data.get(timeout):
                            data.pop(timeout, None)
                    mode = int(data.get('status', 5)) - 5
                    # 7.0対応
                    if self.VERSION.major >= 7.0:
                        # active/passiveのモード判定、7.0に合わせて0:active/1:passive
                        id = self.getKeynameInMethod('proxygroup', 'id')
                        if self.getLatestVersion('MASTER_VERSION') >= 7.0:
                            # プロキシグループのID変換
                            data[id] = self.replaceIdName('proxygroup', data[id])
                            mode = data['operating_mode']
                        else:
                            # 以前のバージョンからの変換
                            data[id] = 0
                            data['name'] = data.pop('host', None)
                            data['allowed_addresses'] = data.pop('proxy_address', None)
                            data['operating_mode'] = mode
                            data.pop('status', None)
                    desc = data.get('description', '')
                    # プロキシの指定記述はdescriptionの先頭に「ZC_WORKER:node;」
                    # ZC_WORKERが無いまたは複数の記述があるプロキシは削除
                    if len(re.findall(ZC_MONITOR_TAG + ':[0-9a-zA-Z-_.]*', desc)) != 1:
                        continue
                    # Discriptionに自分のノード名が載ってないプロキシは削除
                    if not re.match(ZC_MONITOR_TAG + ':%s;' % self.CONFIG.node, desc):
                        if item['NAME'] in self.LOCAL['proxy'].keys():
                            # 自分に割り当てられなくなったプロキシ
                            deleteTarget.append(self.LOCAL['proxy'][item['NAME']]['ZABBIX_ID'])
                            pass
                        continue
                    # PSK利用の判定
                    if mode == 1:
                        # passive
                        usePsk = True if int(data['tls_connect']) == 2 else False
                    else:
                        # active 1:None,2:PSK,4:SSLのビットマップ、2が含まれない1,4,5じゃないことを判定
                        usePsk = True if int(data['tls_accept']) not in [1, 4, 5] else False
                    # 5.4以降はAPIで取れないようになるので設定ファイルに記載で統一
                    if usePsk:
                        psk = self.CONFIG.proxyPsk.get(item['NAME'], [])
                        try:
                            # PSKが16進法か確認
                            int(psk[1], 16)
                            # 適切な長さか確認（128bit以上2048bit以下）
                            if len(psk[1]) < 64 or len(psk[1]) > 1024:
                                psk = []
                        except:
                            psk = []
                        if len(psk) != 2:
                            # PSK情報が不正の場合はPSK未使用設定に変更
                            # プロキシが不在になるとホスト作成の方に処理を入れないといけないので削除はしない
                            if mode:
                                # passive 暗号化なしに変更
                                data['tls_connect'] = 1
                            else:
                                # active PSKフラグの2を引く、2の場合は1にする
                                data['tls_accept'] = int(data['tls_accept']) - 2 if int(data['tls_accept']) > 2 else 1
                            # descriptionにPSK無効にしたことを追記
                            pskDisableMessage = '[%s PSK DISABLED]' % ZABBIX_TIME()
                            if data.get('description'):
                                data['description'] = pskDisableMessage + '\r\n\r\n' + data['description']
                            else:
                                data['description'] = pskDisableMessage
                        else:
                            # PSK情報を設定
                            data['tls_psk_identity'], data['tls_psk'] = psk
                items.append(item)
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, 'Failed processingProxy.')
        self.STORE['proxy'] = items
        # 削除対象がある場合
        if deleteTarget:
            self.STORE['proxyExtend'] = [{'delete': deleteTarget}]
            self.sections['EXTEND'].append('proxyExtend')
        return result

    def processingProxygroup(self):
        '''
        proxygroupのデータ加工
        プロキシグループの削除のみ
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('proxygroup'):
            return (True, 'No Exist Data.')

        deleteTarget = []
        if self.isMaster:
            # マスターノード処理
            pass
        else:
            # ワーカーノード処理
            # 自身が対象ではなくなったプロキシグループの削除
            names = [item['NAME'] for item in self.STORE.get('proxygroup', [])]
            for name, item in self.LOCAL.get('proxygroup', {}).items():
                if name not in names:
                    deleteTarget.append(item['ZABBIX_ID'])
        if deleteTarget:
            self.STORE['proxygroupExtend'] = [{'delete': deleteTarget}]
            self.sections['EXTEND'].append('proxygroupExtend')
        return result

    def processingDrule(self):
        '''
        ストアのネットワークディスカバリデータを加工
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('drule'):
            return (True, 'No Exist Data.')

        # dType
        all = list(range(0,16))
        agent = [9, 10 ,11, 13]
        snmpV1_2 = [10, 11]
        snmpV3 = [13]
        icmp = [12]
        tcp = all
        tcp.remove(12)

        items = []
        try:
            for item in self.STORE['drule'].copy():
                data = item['DATA']
                # プロキシの変換
                # 7.0でプロキシグループに対応していないので、7.2以降変更の可能性あり
                idRename = None
                if self.VERSION.major >= 7.0:
                    # ワーカーノードで7.0未満のマスターノードのデータ
                    if not self.isMaster and self.getLatestVersion('MASTER_VERSION') < 7.0:
                        idName = 'proxy_hostid'
                        idRename = 'proxyid'
                    else:
                        idName = 'proxyid'
                else:
                    idName = 'proxy_hostid'
                id = self.replaceIdName('proxy', data[idName])
                if id is None:
                    # 対応するプロキシーがなければ除外する
                    continue
                if idRename:
                    data[idRename] = id
                    data.pop(idName, None)
                else:
                    data[idName] = id
                if self.isMaster:
                    # マスターノード処理
                    pass
                else:
                    # ワーカーノード処理
                    # 不要データの削除
                    [data.pop(param, None) for param in self.discardParameter['drule']]
                    # 7.0対応
                    data.pop('error', None)
                    for check in data['dchecks']:
                        dType = int(check['type'])
                        # ID系は共通で削除
                        check.pop('dcheckid', None)
                        check.pop('druleid', None)
                        # デフォルト値のものは削除
                        # 4.2追加 host_source, name_source 
                        defaultZaro = ['port', 'host_source', 'name_source']
                        for param in defaultZaro:
                            if int(check.get(param, 0)) == 0:
                                check.pop(param, None)
                        # エージェントタイプ以外では不要
                        if dType not in agent:
                            check.pop('key_')
                        # SNMP v1 or v2以外では不要
                        if dType not in snmpV1_2:
                            check.pop('snmp_community', None)
                        # SNMP v3 以外では不要
                        if dType not in snmpV3:
                            snmpV3Param = [
                                'snmpv3_authpassphrase',
                                'snmpv3_authprotocol',
                                'snmpv3_contextname',
                                'snmpv3_privpassphrase',
                                'snmpv3_privprotocol',
                                'snmpv3_securitylevel',
                                'snmpv3_securityname',
                            ]
                            [check.pop(param, None) for param in snmpV3Param]
                        # ICMP以外では不要
                        if dType not in icmp:
                            # 7.0追加
                            check.pop('allow_redirect', None)
                items.append(item)
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, 'Failed processingDrule.')
        self.STORE['drule'] = items
        return result

    # SLA and service tree

    def processingSla(self):
        '''
        ストアのslaデータを加工
        '''
        result = ZC_COMPLETE

        items = []
        deleteTarget = []
        try:
            for item in self.STORE.get('sla', []).copy():
                data = item['DATA']
                if self.isMaster:
                    # マスターノード処理
                    pass
                else:
                    # ワーカーノード処理
                    # 空データの削除
                    [data.pop(param, None) for param in self.discardParameter['sla'] if not data.get(param)]
                items.append(item)
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, 'Failed processingSla.')
        if not self.isMaster:
            # ワーカー側削除対象
            names = [item['NAME'] for item in self.STORE.get('sla', [])]
            for name, item in self.LOCAL.get('sla', {}).items():
                if name not in names:
                    deleteTarget.append(item['ZABBIX_ID'])
        if items:
            self.STORE['sla'] = items
        if deleteTarget:
            self.STORE['slaExtend'] = [{'delete': deleteTarget}]
            self.sections['EXTEND'].append('slaExtend')
        return result

    def processingService(self):
        '''
        ストアのserviceデータを加工
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('service'):
            return (True, 'No Exist Data.')

        items= []
        extend = []
        deleteTarget = []
        try:
            for item in self.STORE.get('service', []).copy():
                name = item['NAME']
                data = item['DATA']
                if self.isMaster:
                    # masterノード処理
                    # parents/childrenのnameの中身のみのリスト化
                    data['parents'] = [parent['name'] for parent in data['parents']]
                    data['children'] = [child['name'] for child in data['children']]
                else:
                    # ワーカーノード処理
                    # read-onlyの削除
                    [data.pop(param, None) for param in self.discardParameter['service']]
                    # サービス関連性の抜きだし
                    extend.append(
                        {
                            'NAME': name,
                            'DATA': {
                                'parents': [parent for parent in data.pop('parents', [])],
                                'children': [child for child in data.pop('children', [])]
                            }
                        }
                    )
                # 抜き出した後のデータ
                items.append(item)
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, 'Failed processingService.')
        if not self.isMaster:
            # ワーカー側削除対象
            names = [item['NAME'] for item in self.STORE.get('service', [])]
            for name, item in self.LOCAL.get('service', {}).items():
                if name not in names:
                    deleteTarget.append(item['ZABBIX_ID'])
        if items:
            self.STORE['service'] = items
        if extend or deleteTarget:
            self.sections['EXTEND'].append('serviceExtend')
            self.STORE['serviceExtend'] = []
        if extend:
            self.STORE['serviceExtend'].extend(extend)
        if deleteTarget:
            self.STORE['serviceExtend'].append({'delete': deleteTarget})
        return result

    def processingServiceExtend(self):
        '''
        Serviceの副処理
        Service同士の関係を処理processingServiceの後に設定
        Serviceが適用されてから実行しないとデータがない
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('serviceExtend'):
            return (True, 'No Exist Data.')
        # マスターノードで行う処理はない（そもそもないからここを通らないはず）
        if self.isMaster:
            return result

        items = []
        try:
            idName = self.getKeynameInMethod('service', 'id')
            for item in self.STORE['serviceExtend'].copy():
                if not item.get('delete'):
                    data = item['DATA']
                    # parents/childrenのID変換
                    children = data.get('children', [])
                    parents = (
                        [
                            {
                                idName: self.replaceIdName('service', parent)
                            } for parent in data.get('parents', [])
                        ]    
                    )
                    children = (
                        [
                            {
                                idName: self.replaceIdName('service', child)
                            } for child in data.get('children', [])
                        ]
                    )
                    data.update(
                        {
                            'parents': parents,
                            'children': children
                        }
                    )
                items.append(item)
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, 'Failed processingServiceExtend.')
        self.STORE['serviceExtend'] = items
        return result

    # Event correlation

    def processingCorrelation(self):
        '''
        ストアのcorrelationデータを加工
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('correlation'):
            return (True, 'No Exist Data.')

        items = []
        try:
            for item in self.STORE['correlation'].copy():
                # 加工が必要なのはfilter内の項目のみ
                filter = item['DATA']['filter']
                # 読み取り専用削除
                filter.pop('eval_formula', None)
                # 不要項目の削除
                if int(filter['evaltype']) != 3:
                    # カスタム条件式以外では不要
                    filter.pop('formula', None)
                # 条件要素内の処理
                idName = self.getKeynameInMethod('hostgroup', 'id')
                for condition in filter['conditions'].copy():
                    # カスタム条件式以外では不要
                    if int(filter['evaltype']) != 3:
                        condition.pop('formulaid', None)
                    # ホストグループ対象（type == 2）のみID変換が必要
                    if int(condition['type']) == 2:
                        id = self.replaceIdName('hostgroup', condition[idName])
                        if id:
                            condition[idName] = id
                        else:
                            filter['conditions'].remove(condition)
                if len(filter['conditions']) == 0:
                    # 条件要素がすべてなくなってしまったものは削除
                    continue
                items.append(item)
        except Exception as e:
            self.LOGGER.debug(e)
            result = (False, 'Failed processingCorrelation.')
        self.STORE['correlation'] = items
        return result

    # Account, role, and authentication

    def processingUser(self):
        '''
        ストアのuserデータを加工
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('user'):
            return (True, 'No Exist Data.')
        
        items = []
        deleteTarget = []
        try:
            for item in self.STORE['user'].copy():
                data = item['DATA']
                # Media設定のID変換
                for media in data['medias'].copy():
                    # ID変換
                    idName = self.getKeynameInMethod('mediatype', 'id')
                    id = self.replaceIdName('mediatype', media[idName])
                    if id:
                        media.update({idName: id})
                    else:
                        # ID変換できないメディアは削除
                        data['medias'].remove(media)
                # 5.2 対応
                # ユーザー権限がtype -> roleになるのでID変換が必要になる
                if self.VERSION.major >= 5.2:
                    permitMethod = 'role'
                    permit = self.getKeynameInMethod(permitMethod, 'id')
                    data[permit] = self.replaceIdName(permitMethod, data.get(permit))
                    if not self.isMaster and self.getLatestVersion('MASTER_VERSION') < 5.2:
                        # 5.2以前は変換の必要がなかったので変換しないで代入
                        data[permit] = data.pop('type')
                else:
                    permit = 'type'
                usrgrps = 'usrgrps'
                if self.isMaster:
                    # 所属Usergroup処理
                    # usrgrps:[]の中を{'name': 'xxxx'}のバリューだけにする
                    data[usrgrps] = [param['name'] for param in data.get(usrgrps, []) if param.get('name')]
                else:
                    # 認証サービスからの登録ユーザーは除外
                    if int(data.get('userdirectoryid', 0)):
                        continue
                    # 特権管理者の複製許可確認
                    if not self.CONFIG.cloningSuperAdmin:
                        if data[permit] == ZABBIX_SUPER_ROLE:
                            continue
                    # 複製許可ユーザーの確認
                    if self.getLatestVersion('MASTER_VERSION') >= 5.4:
                        idName = self.getKeynameInMethod('user', 'name')
                    else:
                        idName = 'alias'
                    password = self.CONFIG.enableUser.get(data[idName])
                    if not password:
                        continue
                    # パスワード設定を新規作成ユーザーに追加、既存ユーザーはパスワード変更はできない（元がわからない）
                    if item['NAME'] not in self.LOCAL['user'].keys():
                        data['passwd'] = password
                    # usrgrps:[]の中を{'usrgrpid': id}に変換する
                    idName = self.getKeynameInMethod('usergroup', 'id')
                    data[usrgrps] = [
                        {
                            idName: self.replaceIdName('usergroup', param)
                        } for param in data.get(usrgrps, [])
                    ]
                    # 不要項目削除
                    data.pop('userdirectoryid', None)
                    data.pop('users_status', None)
                    data.pop('gui_access', None)
                    data.pop('debug_mode', None)
                    medias = data.pop('medias', [])
                    addMedias = []
                    for media in medias.copy():
                        # 不要項目削除
                        media.pop('mediaid', None)
                        media.pop('userid', None)
                        # 7.0対応
                        if int(media.get('userdirectory_mediaid', 0)):
                            # 認証システムからの指定登録メディアは除外
                            medias.pop(media)
                            continue
                        media.pop('userdirectory_mediaid', None)
                        addMedias.append(media)
                    if addMedias:
                        if self.VERSION.major >= 5.2:
                            data['medias'] = addMedias
                        else:
                            data['user_medias'] = addMedias
                items.append(item)
        except Exception as e:
            self.LOGGER.debug(e)
            return (False, 'Failed processingUser.')
        # マスターデータにないワーカーのユーザー削除指定
        if not self.isMaster:
            name = self.getKeynameInMethod('user', 'name')
            # ワーカー側削除対象
            users = [item['NAME'] for item in self.STORE.get('user', [])]
            for user, item in self.LOCAL.get('user', {}).items():
                if item['DATA'][name] == ZABBIX_SUPER_USER:
                    # Adminはスキップ
                    continue
                if user not in users:
                    deleteTarget.append(item['ZABBIX_ID'])
        self.STORE['user'] = items
        if deleteTarget:
            self.STORE['userExtend'] = [{'delete': deleteTarget}]
            self.sections['EXTEND'].append('userExtend')
        return result

    def processingUsergroup(self):
        '''
        ストアのusergroupデータを加工
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('usergroup'):
            return (True, 'No Exist Data.')

        items = []
        try:
            for item in self.STORE['usergroup'].copy():
                data = item['DATA']
                # 共通
                # tag_filtersのgroupidを変換
                idName = self.getKeynameInMethod('hostgroup', 'id')
                for tag in data.get('tag_filters', []):
                    [
                        tag.update(
                            {
                                idName: self.replaceIdName('hostgroup', tag[idName])
                            }
                        )
                    ]
                # rightsのidを変換
                # 6.2対応
                if self.VERSION.major >= 6.2:
                    targets = ['hostgroup', 'templategroup']
                else:
                    targets = ['']
                for target in targets:
                    rKey = '_'.join([target, 'rights']).lstrip('_')
                    if self.isMaster:
                        pass
                    else:
                        if self.getLatestVersion('MASTER_VERSION') < 6.2:
                            # ワーカーノード処理
                            # マスターノードが6.2以前はrightsが分離されていないので*group_rightsにはrightsの内容を設定する
                            rKey = 'rights'
                    rights = data.get(rKey)
                    # 空ならスキップ
                    if not rights:
                        continue
                    # 初期化
                    data[rKey] = []
                    # 6.0以前の場合はNoneなのでhostgroupにする
                    if target is None:
                        target = 'hostgroup'
                    for val in rights:
                        # ID変換で値が返ってくるもののみを変換する
                        id = self.replaceIdName(target, val['id'])
                        if id:
                            data[rKey].append(
                                {
                                    'id': id,
                                    'permission': val['permission']
                                }
                            )
                if self.isMaster:
                    # マスターノード処理
                    pass
                else:
                    # ワーカーノード処理
                    # 6.2対応
                    if self.VERSION.major >= 6.2:
                        # 0の場合不要
                        if not int(data.get('userdirectoryid', 0)):
                            data.pop('userdirectoryid', None)
                        # 内部認証（１）とフロントエンドアクセス禁止（３）では不要
                        if int(data.get('gui_access')) in [1, 3]:
                            data.pop('userdirectoryid', None)
                    # 7.0対応
                    if self.VERSION.major >= 7.0:
                        # MFAを使わないのであれば不要
                        if not data.get('mfa_status'):
                            data.pop('mfa_status', None)
                            data.pop('mfaid', None)
                    # 所属するユーザーのリストはusergroupには要らない（userの方で処理される）
                    data.pop('users', None)
                    data.pop('userids', None)
                    if not data.get('tag_filters'):
                        # 空の場合は項目を消す
                        data.pop('tag_filters', None)
                items.append(item)
        except Exception as e:
            self.LOGGER.debug(e)
            return (False, 'Failed processingUsergroup.')
        self.STORE['usergroup'] = items
        return result

    def processingRole(self):
        '''
        ストアのroleデータを加工
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('role'):
            return (True, 'No Exist Data.')
        
        items = []
        try:
            for item in self.STORE['role'].copy():
                data = item['DATA']
                if self.isMaster:
                    # マスターノード処理
                    pass
                else:
                    # ワーカーノード処理
                    # 不要項目を削除
                    for param in data.copy().keys():
                        if param in self.discardParameter['role']:
                            data.pop(param, None)
                    rules = data['rules']
                    for rule, params in rules.copy().items():
                        if rule in self.discardParameter['role']:
                            rules.pop(rule)
                            continue
                        if isinstance(params, list):
                            for param in params:
                                if param.get('name') in self.discardParameter['role']:
                                    rules[rule].remove(param)
                    if self.VERSION.major >= 6.4:
                        # configuration.actionsの分割
                        value = 0
                        for param in data['rules']['ui'].copy():
                            if param.get('name') == 'configuration.actions':
                                value = int(param['status'])
                                data['rules']['ui'].remove(param)
                        if value and self.getLatestVersion('MASTER_VERSION') < 6.4:
                            data['rules']['ui'].extend(
                                [
                                    {'name': 'configuration.trigger_actions', 'status': value},
                                    {'name': 'configuration.service_actions', 'status': value},
                                    {'name': 'configuration.discovery_actions', 'status': value},
                                    {'name': 'configuration.autoregistration_actions', 'status': value},
                                    {'name': 'configuration.internal_actions', 'status': value},
                                ]
                            )
                    if self.CONFIG.zabbixCloud:
                        # ZabbixCloud対応: Module関連が存在しない
                        for param in self.zabbixCloudSpecialItem['role']:
                            item['DATA']['rules'].pop(param, None)
                items.append(item)
        except Exception as e:
            self.LOGGER.debug(e)
            return (False, 'Failed processingRole.')
        self.STORE['role'] = items
        return result

    def processingUserdirectory(self):
        '''
        ストアのuserdirectoryデータを加工
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('userdirectory'):
            return (True, 'No Exist Data.')
        
        items = []
        try:
            for item in self.STORE['userdirectory'].copy():
                data = item['DATA']
                # JITプロビジョンのメディアとユーザーグループ割り当てのID変換
                if data.get('provison_media'):
                    idName = self.getKeynameInMethod('meidatype', 'id')
                    for provMedia in data['provision_media'].copy():
                        # 不要項目の削除
                        provMedia.pop('userdirectory_mediaid', None)
                        id = self.replaceIdName('meidatype', provMedia[idName])
                        # メディアが存在しなかったら削除
                        if id:
                            provMedia[idName] = id
                        else:
                            data['provision_media'].pop(provMedia, None)
                if data.get('provision_groups'):
                    for provUgroup in data['provision_groups'].copy():
                        # roleのID変換
                        idName = self.getKeynameInMethod('role', 'id')
                        provUgroup['roleid'] = self.replaceIdName('role', provUgroup['roleid'])
                        # usergroupのID変換
                        idName = self.getKeynameInMethod('usergroup', 'id')
                        for ugrp in provUgroup['user_group'].copy():
                            id = self.replaceIdName('usergroup', ugrp[idName])
                            if not id:
                                provUgroup['user_group'].pop(ugrp, None)
                                continue
                            else:
                                ugrp[idName] = id
                        # ユーザーグループが空になっていたら設定内リストから削除
                        if len(provUgroup['user_group']) == 0:
                            data['provision_groups'].remove(provUgroup)
                if self.isMaster:
                    # マスターノード処理
                    pass
                else:
                    # ワーカーノード処理
                    # 割り当てメディア設定が空になっていたら削除
                    if not data.get('provison_media'):
                        data.pop('provison_media', None)
                    # 割り当てグループ設定が空になっていたら削除
                    if not data.get('provision_groups'):
                        data.pop('provision_groups', None)
                items.append(item)
        except Exception as e:
            self.LOGGER.debug(e)
            return (False, 'Failed processingUserdirectory.')
        self.STORE['userdirectory'] = items
        return result

    def processingMfa(self):
        '''
        ストアのMFAデータを加工
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('mfa'):
            return (True, 'No Exist Data.')
        
        items = []
        try:
            for item in self.STORE['mfa'].copy():
                data = item['DATA']
                mfaType = int(data['type'])
                if self.isMaster:
                    # マスターノード処理
                    pass
                else:
                    # ワーカーノード処理
                    # typeごとに不要な要素は削除
                    if mfaType == 1:
                        # TOTP
                        data.pop('api_hostname', None)
                        data.pop('clientid', None)
                        data.pop('client_secret', None) # ないはずだけど一応
                    elif mfaType == 2:
                        # Duo Universal Prompt
                        data.pop('hash_function', None)
                        data.pop('code_length', None)
                        # Duo Universal Promptのシークレットは設定から読み込む
                        name = data[self.getKeynameInMethod('mfa', 'name')]
                        secret = self.CONFIG.mfaClientSecret.get(name)
                        if secret:
                            data['client_secret'] = secret
                        else:
                            continue
                    else:
                        continue
                items.append(item)
        except Exception as e:
            self.LOGGER.debug(e)
            return (False, 'Failed processingMfa.')
        self.STORE['mfa'] = items
        return result

    # External integration

    def processingConnector(self):
        '''
        ストアのConnectorデータを加工
        '''
        result = ZC_COMPLETE
        if not self.STORE.get('connector'):
            return (True, 'No Exist Data.')
        
        items = []
        deleteTarget = []
        try:
            for item in self.STORE['connector'].copy():
                data = item['DATA']
                name = item['NAME']
                if self.isMaster:
                    # IDの変換関連
                    pass
                else:
                    if self.CONFIG.disableMonitoring:
                        # 監視無効設定がある場合はすべて無効にする
                        data['status'] = ZABBIX_DISABLE
                    # 多分今後protocolが増えると要不要の判断が必要になると思う
                    # if int(data.get('protocol', 0)) > 0:
                    # 送信種別
                    if int(data.get('data_type', 0)) == 1:
                        # Eventで不要要素の削除
                        data.pop('item_value_type', None)
                    # 試行回数インターバル
                    if int(data.get('max_attempts', 1)) == 1:
                        data.pop('attempt_interval', None)
                    # 認証情報ごとの必要な情報以外の削除
                    auth = int(data.get('authtype', 0))
                    if auth:
                        if auth == 5:
                            # Bearer
                            data.pop('username', None)
                            data.pop('password', None)
                        else:
                            # Basic:1 / NTLM:2 / Kerberos:3 / Digest:4
                            data.pop('token', None)
                items.append(item)
            connectors = [item['NAME'] for item in items]
            for connector, item in self.LOCAL.get('connector', {}).items():
                if connector not in connectors:
                    deleteTarget.append(item['ZABBIX_ID'])
        except Exception as e:
            self.LOGGER.debug(e)
            return (False, 'Failed processingConnector.')
        self.STORE['connector'] = items
        if deleteTarget:
            self.STORE['connectorExtend'] = [{'delete': deleteTarget}]
            self.sections['EXTEND'].append('connectorExtend')
        return result

    # Authentication settings

    def processingAuthentication(self):
        '''
        ストアのAuthenticationデータを加工
        マスターノードのみ、ID変換処理をここで行う
        適用はsetAuthenticationToZabbix()で行う
        例外的な処理はあんまりやりたくないけどしゃあない
        '''
        if not self.STORE.get('authentication'):
            return (True, 'No Exist Data.')
        
        for item in self.STORE['authentication']:
            data = item['DATA']
            if item['NAME'] == 'disabled_usrgrpid':
                # LDAP/SAMLで使うdisabled_usrgrpidのID変換
                id = self.replaceIdName('usergroup', data['disabled_usrgrpid'])
                if id:
                    data['disabled_usrgrpid'] = id
            elif item['NAME'] == 'mfaid':
                # MFAのデフォルト利用のID変換
                id = self.replaceIdName('mfa', data['mfaid'])
                if id:
                    data['mfaid'] = id
            else:
                pass

        return ZC_COMPLETE
