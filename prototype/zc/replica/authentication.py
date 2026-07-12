#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common import *

class ReplicaAuthenticationMixin:
    def setAuthenticationToZabbix(self):
        '''
        Zabbixの認証設定を変更する
        MFAでID変換の必要が入ってきたので独立して最後に実行
        '''
        if not self.STORE.get('authentication'):
            # 認証のAPIがないバージョンではデータがないのでスキップ
            return (True, 'Skip, No Exsit Authentication Data.')
        
        PRINT_PROG(f'{TAB*2}Processing Authentication Data:\n', self.CONFIG.quiet)

        # 認証設定
        data = {}
        [data.update(item['DATA']) for item in self.STORE['authentication']]
        # 6.2以下対応
        # ディレクトリサービス認証を使用しない場合は削除
        if self.VERSION.major <= 6.2:
            if not int(data.get('idap_configured', 0)):
                for param in self.discardParameter['authentication']['ldap']:
                    data.pop(param, None)
                data.pop('idap_configured', None)
                PRINT_TAB(3, self.CONFIG.quiet)
                self.LOGGER.info('Drop LDAP Enable: Done.')
            if not int(data.get('saml_auth_enabled')):
                for param in self.discardParameter['authentication']['saml']:
                    data.pop(param, None)
                data.pop('saml_auth_enabled', None)
                PRINT_TAB(3, self.CONFIG.quiet)
                self.LOGGER.info('Drop SAML Enable: Done.')

        # 6.2対応
        if self.VERSION.major >= 6.2:
            if self.VERSION.major == 6.2:
                # バグってLDAPサーバーが設定されていないと０でも１でも弾くので削除、バーカバーカ
                data.pop('authentication_type', None)
            if self.getLatestVersion('MASTER_VERSION') < 6.2:
                # 6.2以降、userdirectoryが新設されてLDAP設定がそちらに移動
                if int(data.get('ldap_configured')):
                    ldapParams = {
                        'name': 'LDAP Converted 6.0 -> 6.2 later by ZC'
                    }
                    for param in self.discardParameter['authentication']['ldap']:
                        value = data.pop(param, '').replace('ldap_', '')
                        if value:
                            ldapParams.update(
                                {
                                    param.replace('ldap_', ''): value
                                }
                            )
                    if ldapParams.get('host'):
                        process = 'Move LDAP Setting -> UserDirectory'
                        PRINT_TAB(3, self.CONFIG.quiet)
                        try:
                            res = self.ZAPI.userdirectory.create(**ldapParams)
                            data['ldap_auth_enabled'] = 1
                            data['ldap_userdirectoryid'] = res['userdirectoryids'][0]
                            self.LOGGER.info(f'{process}: Success.')
                        except:
                            data['ldap_auth_enabled'] = 0
                            self.LOGGER.error(f'{process}: Failed.')

        # 6.4対応
        if self.VERSION.major >= 6.4:
            # 古いバージョンのパラメーターだったら変換
            value = data.pop('ldap_configured', None)
            if value:
                data.update({'ldap_auth_enabled': int(value)})
            if self.getLatestVersion('MASTER_VERSION') < 6.4:
                # SAMLがuserdirectoryに移動
                if int(data.get('saml_auth_enabled', 0)):
                    samlParams = {
                        'name': 'SAML Converted 6.0/6.2 -> 6.4 later by ZC',
                        'idp_type': 1
                    }
                    for param in self.discardParameter['authentication']['saml']:
                        value = data.pop(param, '').replace('saml_', '')
                        if value:
                            samlParams.update(
                                {
                                    param.replace('saml_', ''): value
                                }
                            )
                    if samlParams.get('idp_entityid'):
                        process = 'Move SAML Setting -> UserDirectory'
                        PRINT_TAB(3, self.CONFIG.quiet)
                        try:
                            res = self.ZAPI.userdirectory.create(**samlParams)
                            self.LOGGER.info(f'{process}: Success.')
                        except:
                            data['saml_auth_enabled'] = 0
                            self.LOGGER.error(f'{process}: Failed.')
            # LDAP利用しない
            if int(data.get('ldap_auth_enabled', 0)) == 0:
                ldap = False
                for param in self.discardParameter['authentication']['ldap']:
                    data.pop(param, None)
                data.pop('ldap_auth_enabled', None)
            else:
                ldap = True
            # SAML利用しない
            if int(data.get('saml_auth_enabled', 0)) == 0:
                saml = False
                for param in self.discardParameter['authentication']['saml']:
                    data.pop(param, None)
                data.pop('saml_auth_enabled', None)
            else:
                saml = True
            if ldap or saml:
                # LDAP/SAMLどちらかを利用する場合は変換する
                userGroup = data['disabled_usrgrpid']
                id = self.replaceIdName('usergroup', userGroup)
                if id:
                    data['disabled_usrgrpid'] = id
                    PRINT_TAB(3, self.CONFIG.quiet)
                    self.LOGGER.info(f'Data Convert[disabled_usrgprid({userGroup})]: Done.')
            else:
                # LDAP/SAMLどちらも利用しない場合
                data.pop('disabled_usrgrpid', None)

        # 7.0対応
        if self.VERSION.major >= 7.0:
            # MFA利用
            if int(data.get('mfa_status', 0)) == 0:
                data.pop('mfa_status', None)
                data.pop('mfaid', None)
            else:
                # デフォルト利用のMFAのID変換処理
                useMfa = data['mfaid']
                id = self.replaceIdName('mfa', useMfa)
                if id:
                    data['mfaid'] = id
                    PRINT_TAB(3, self.CONFIG.quiet)
                    self.LOGGER.info(f'Data Convert[mfaid({useMfa})]: Done.')

        # ZabbixCloud対応: HTTP AUTH関連が存在しない
        if self.CONFIG.zabbixCloud:
            for property in self.zabbixCloudSpecialItem['authentication']:
                data.pop(property, None)
                PRINT_TAB(3, self.CONFIG.quiet)
                self.LOGGER.info(f'Drop Parameters for ZabbixCloud[{property}]: Done.')
        try:
            self.ZAPI.authentication.update(**data)
        except Exception as e:
            self.LOGGER.debug(e)
            return (False, f'Failed Set Authentication.')

        return ZC_COMPLETE
