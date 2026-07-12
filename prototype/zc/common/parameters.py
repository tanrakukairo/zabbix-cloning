#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common.constants import *
from zc.common.utils import *
from zc.common import __LOGGER__

class ZabbixCloneParameter():
    '''
    ZabbixAPIのパラメータのバージョン間差異を吸収するクラス
    '''

    def __init__(self, version, logger):
        # loggerインスタンス
        logConfig = DEFAULT_LOG
        logConfig['logLevel'] = 'ERROR'
        self.LOGGER = logger if logger else __LOGGER__(**logConfig)

        if version:
            version = {
                'major': version.major,
                'minor': version.minor
            }
        else:
            version = {
                'major': ZC_DEFAULT_ZABBIX_VERSION,
                'minor': 0
            }

        if version['major'] < ZC_SUPPORT_VERSION_LOWER:
            sys.exit('Out of support version: %s' % version['major'])

        # ベース:4.0
        methodParameters = {
            'hostgroup': {
                'id': 'groupid',
                'name': 'name',
                'options': {
                    'output': 'extend',
                },
            },
            'host': {
                'id': 'hostid',
                'name': 'host',
                'options': {
                    'output': ['hostid', 'host'],
                    'selectTags': ['tag', 'value']
                },
            },
            'template': {
                'id': 'templateid',
                'name': 'name',
                'options': {
                    'output': ['templateid', 'name'],
                },
            },
            'user': {
                'id': 'userid',
                'name': 'alias',
                'options': {
                    'output': ['alias', 'type'],
                    'getAccess': True,
                    'selectUsrgrps': ['name'],
                    'selectMedias': 'extend'
                },
            },
            'usergroup': {
                'id': 'usrgrpid',
                'name': 'name',
                'options': {
                    'output': 'extend',
                    'selectTagFilters': 'extend',
                    'selectRights': 'extend'
                }
            },
            'usermacro': {
                # ユーザーマクロはConfigurationでhostsの中に入ってくるのでここではグローバルマクロのみを対象とする
                'id': 'globalmacroid',
                'name': 'macro',
                'options': {
                    'output': ['macro', 'value'],
                    'globalmacro': True
                }
            },
            'mediatype': {
                'id': 'mediatypeid',
                'name': 'description', 
                'options': {
                    'output': 'extend',
                },
            },
            'action': {
                'id': 'actionid',
                'name': 'name',
                'options': {
                    'output': 'extend',
                    'selectOperations': 'extend',
                    'selectRecoveryOperations': 'extend',
                    'selectAcknowledgeOperations': 'extend',
                    'selectFilter': 'extend',
                    'search': {'conditiontype': [2]}, #トリガー直接指定のフィルターを除外
                },
            },
            'maintenance': {
                'id': 'maintenanceid',
                'name': 'name',
                'options': {
                    'selectGroups': 'extend',
                    'selectHosts': 'extend',
                    'selectTimeperiods': 'extend',
                    'selectTags': 'extend'
                },
            },
            'script': {
                'id': 'scriptid',
                'name': 'name',
                'options': {
                }
            },
            'valuemap': {
                'id': 'valuemapid',
                'name': 'name',
                'options': {
                    'output': 'extend',
                    'selectMappings': 'extend'
                }
            },
            'proxy': {
                'id': 'proxyid',
                'name': 'host',
                'options': {
                    'output': [ # PSK鍵をストアに保存しないようにするためAPIで取得しない
                        'host',
                        'status',
                        'proxy_address',
                        'tls_connect',
                        'tls_accept',
                        'tls_issuer',
                        'tls_subject',
                        'description'
                    ],
                    'selectInterface': ['useip', 'ip', 'dns', 'port']
                },
            },
            'drule': { # ネットワークディスカバリ
                'id': 'druleid',
                'name': 'name',
                'options': {
                    'output': 'extend',
                    'selectDChecks': 'extend'
                }
            },
            'correlation': {
                'id': 'correlationid',
                'name': 'name',
                'options': {
                    'output': 'extend',
                    'selectOperations': 'extend',
                    'selectFilter': 'extend'
                }
            },
            # Triggerはテンプレートに紐づいているものだけしかとらないのでAPIで取得しないようここには設定しない
        }

        # ベース:4.0
        sections = {
            # 一般設定グループ
            'GLOBAL': [],
            # Configuration.export操作グループ（{Method名: ファイル内Section名}）
            'CONFIG_EXPORT': {
                'hostgroup': 'groups',
                'template': 'templates',
                'host': 'hosts',
                'valuemap': 'valueMaps',
                'trigger': 'triggers',
            },
            # Configration.import操作グループ（名前が変わっても同じメソッドに変換する）
            'CONFIG_IMPORT': {},
            # Configurationの前に実行するグループ（Method名）
            'PRE': [
                'usermacro',
                'mediatype',
                'proxy',
            ],
            # Configurationの中間（templateとhostの間）に実行するグループ（Method名）
            'MID': [
                'script',
            ],
            # Configuration後に実行するグループ（Method名）
            'POST': [
                'action',
                'maintenance',
                'drule',
                'correlation',
            ],
            # アカウント関連の処理をするグループ（Method名）
            'ACCOUNT': [
                'usergroup', 
                'user',
            ],
            # 最後に実行される特別処理のグループ（Method名）
            'EXTEND': [],
            # DBダイレクト操作グループ（テーブル名）
            'DB_DIRECT': [
                'regexps',
                'expressions',
                'config',
            ],
        }

        # ベース:4.0
        importRules = {
            'applications': {
                'createMissing': True,
                'deleteMissing': True,
            },
            'groups': {
                'createMissing': True
            },
            'hosts': {
                'createMissing': True,
                'updateExisting': True,
            },
            'templateLinkage': {
                'createMissing': True,
                'deleteMissing': True,
            },
            'templates': {
                'createMissing': True,
                'updateExisting': True,
            },
            'items': {
                'createMissing': True,
                'updateExisting': True,
                'deleteMissing': True,
            },
            'discoveryRules': {
                'createMissing': True,
                'updateExisting': True,
                'deleteMissing': True,
            },
            'triggers': {
                'createMissing': True,
                'updateExisting': True,
                'deleteMissing': True,
            },
            'valueMaps': {
                'createMissing': True,
                'updateExisting': True,
            },
            # 以下、現在未対応なのでインポート操作しない
            'images': {
                'createMissing': False,
                'updateExisting': False,
            },
            'maps': {
                'createMissing': False,
                'updateExisting': False,
            },
            'screens': {
                'createMissing': False,
                'updateExisting': False,
            },
            'graphs': {
                'createMissing': False,
                'updateExisting': False,
                'deleteMissing': False,
            },
            'templateScreens': {
                'createMissing': False,
                'updateExisting': False,
                'deleteMissing': False,
            },
            'httptests': {
                'createMissing': False,
                'updateExisting': False,
                'deleteMissing': False,
            },
        }

        # 破棄するパラメーター
        discardParameter = {
            'host': ['items', 'triggers', 'discovery_rules'],
            'action': ['actionid', 'operationid', 'opcommand_hstid', 'opcommand_grpid'],
            'proxy': ['interface', 'lastaccess', 'version', 'compatibility', 'state', 'auto_compress'],
            'drule': ['nextcheck'],
            'authentication': {
                'ldap': [
                    'ldap_host',
                    'ldap_port',
                    'ldap_base_dn',
                    'ldap_search_attribute',
                    'ldap_bind_dn',
                    'ldap_case_sensitive',
                    'ldap_bind_password',
                    'ldap_userdirectoryid',
                    'ldap_jit_status',
                    'jit_provision_interval',
                ],
                'saml': [
                    'saml_idp_entityid',
                    'saml_sso_url',
                    'saml_slo_url',
                    'saml_username_attribute',
                    'saml_sp_entityid',
                    'saml_nameid_format',
                    'saml_sign_messages',
                    'saml_sign_assertions',
                    'saml_sign_authn_requests',
                    'saml_sign_logout_requests',
                    'saml_sign_logout_responses',
                    'saml_encrypt_nameid',
                    'saml_encrypt_assertions',
                    'saml_case_sensitive',
                    'saml_jit_status',
                ]
            }
        }

        # CONFIG_IMPORTの生成
        sections['CONFIG_IMPORT'][4.0] = {}
        for method, section in sections['CONFIG_EXPORT'].items():
            if method == 'valuemap':
                section = 'value_maps'
            sections['CONFIG_IMPORT'][4.0].update({section: method})

        # メジャーバージョンアップで追加されたメソッド、下位バージョンはこれみてスキップする
        addMethods = {}

        # DB直接操作、削除されたカラム
        dbConfigDropCols = {}

        # DB直接操作、configテーブルのカラム名変更
        dbConfigRenameCols = {}

        # 7.0新機能 個別のタイムアウト設定
        timeoutTarget = []

        # 7.0以降対応
        # ZabbixCloudで対応が必要な要素
        zabbixCloudSpecialItem = {
            'mediatype': [
                'Cloud Email'
            ],
            'role': [
                'modules',
                'modules.default_access'
            ],
            'authentication': [
                'http_auth_enabled',
                'http_login_form',
                'http_strip_domains',
                'http_case_sensitive'
            ]
        }

        # 4.4対応
        addMethods[4.4] = ['autoregistration']
        if version['major'] >= 4.4:
            # グローバル設定の自動登録設定のAPI化
            methodParameters.update(
                {
                    'autoregistration': {
                        'id': None,
                        'name': None,
                        'options': {}
                    }
                }
            )
            sections['GLOBAL'].append('autoregistration')
            # METHOD:mediatypeのキー名description->name
            # MediaTypeのAPI -> CONFIG_EXPORT移動
            methodParameters['mediatype']['name'] = 'name'
            methodParameters['mediatype']['options']['output'] = ['name']
            sections['PRE'].remove('mediatype')
            sections['CONFIG_EXPORT'].update({'mediatype': 'mediaTypes'})
            sections['CONFIG_IMPORT'][4.4] = {}
            sections['CONFIG_IMPORT'][4.4].update({'mediaTypes': 'mediatype'})
            importRules.update(
                {
                    'mediaTypes': {
                        'createMissing': True,
                        'updateExisting': True
                    }
                }
            )

        # 5.0対応
        if version['major'] >= 5.0:
            # usermacroにtype追加、textにのみ対応、secretはzc.conf読み込みで対応
            methodParameters['usermacro']['options']['filter'] = {'type': 0}
            # 不要になったカラム
            dbConfigDropCols.update(
                {
                    5.0: [
                        'dropdown_first_entry',
                        'dropdown_first_remember'
                   ]
                }
            )

        # 5.2対応
        # 追加Method
        addMethods[5.2] = ['role']
        if version['major'] >= 5.2:
            # usermacroにtype追加、vaultにも対応
            methodParameters['usermacro']['options']['filter'] = {'type': [0, 2]}
            # 権限管理がroleで詳細設定の追加、対象管理は引き続きusergroup
            methodParameters.update(
                {
                    'role': {
                        'id': 'roleid',
                        'name': 'name',
                        'options': {
                            'output': 'extend',
                            'selectRules': 'extend'
                        }
                    }
                }
            )
            # userの出力にroleidを追加
            methodParameters['user']['options']['output'].append('roleid')
            sections['POST'].append('role')
            # インポートルールtemplateScreens->templateDashboards
            importRules['templateDashboards'] = importRules.pop('templateScreens', {})
            # 不要になったカラム
            dbConfigDropCols.update(
                {
                    5.2: [
                        'refresh_unsupported'
                    ]
                }
            )
            discardParameter['role'] = ['readonly']

        # 5.4対応
        # 追加Method
        addMethods[5.4] = []
        if version['major'] >= 5.4:
            # METHOD:userのキー名変更、alias->username
            methodParameters['user']['name'] = 'username'
            methodParameters['user']['options']['output'] = ['username', 'roleid']
            # valuemapのホスト／テンプレート内への埋め込みによる項目削除（インポートルールは継続）
            sections['CONFIG_EXPORT'].pop('valuemap', None)
            # application/screens廃止に伴うインポートルールの削除
            importRules.pop('applications', None)
            importRules.pop('screens', None)
            # 不要になったカラム
            dbConfigDropCols.update(
                {
                    5.4: [
                        'compression_availability'
                    ]
                }
            )

        # 6.0対応
        # 追加Method
        addMethods[6.0] = ['authentication', 'regexp', 'settings', 'sla', 'service']
        if version['major'] >= 6.0:
            # 認証設定authenticationの追加、5.2で追加されたAPIだけど、設定のテーブルは同じconfigなので6.0で適用
            # グローバル設定のAPI化対応regexp/settings
            # SLA/Service追加、6.0で作り直されているのでそれ以降をサポート
            methodParameters.update(
                {
                    'authentication': {
                        'id': None,
                        'name': None,
                        'options': {}
                    },
                    'regexp': {
                        'id': 'regexpid',
                        'name': 'name',
                        'options': {
                            'output': ['regexpid', 'name'],
                            'selectExpressions': [
                                'expression', 
                                'expression_type', 
                                'exp_delimiter', 
                                'case_sensitive'
                            ]
                        }
                    },
                    'settings': {
                        'id': None,
                        'name': None,
                        'options': {}
                    },
                    'sla': {
                        'id': 'slaid',
                        'name': 'name',
                        'options': {
                            'output': 'extend',
                            'selectSchedule': 'extend',
                            'selectExcludedDowntimes': 'extend',
                            'selectServiceTags': 'extend',
                        }
                    },
                    'service': {
                        'id': 'serviceid',
                        'name': 'name',
                        'options': {
                            'output': 'extend',
                            'selectParents': ['name'],
                            'selectChildren': ['name'],
                            'selectStatusRules': 'extend',
                            'selectProblemTags': 'extend',
                            'selectTags': 'extend',
                        }
                    }
                }
            )
            # パラメータ名変更対応
            value = methodParameters['action']['options'].pop('selectAcknowledgeOperations', None)
            methodParameters['action']['options']['selectUpdateOperations'] = value
            # setGlobalsettingsで実行するグループ
            sections['GLOBAL'].extend(['settings', 'authentication'])
            sections['PRE'].append('regexp')
            sections['POST'].extend(['service', 'sla'])
            # グローバル設定と正規表現のAPI対応に伴うDBのダイレクト操作の廃止
            sections.pop('DB_DIRECT', None)
            discardParameter.update(
                {
                    'service': ['status', 'uuid', 'created_at', 'readonly'],
                    'settings': ['ha_failover_delay'],
                    'sla': ['service_tags', 'schedule', 'excluded_downtimes'],
                }
            )

        # 6.2対応
        # 追加Method
        addMethods[6.2] = ['templategroup']
        if version['major'] >= 6.2:
            # グループがホストとテンプレートでメソッド分離
            # テンプレートグループ追加
            methodParameters.update(
                {
                    'templategroup': {
                        'id': 'groupid',
                        'name': 'name',
                        'options': {
                            'output': 'extend'
                        }
                    }
                }                
            )
            # Maitenanceのホストグループ指定ワードの変更
            value = methodParameters['maintenance']['options'].pop('selectGroups', None)
            methodParameters['maintenance']['options']['selectHostGroups'] = value
            # Usergroupの権限指定ワードの変更
            value = methodParameters['usergroup']['options'].pop('selectRights', None)
            methodParameters['usergroup']['options'].update(
                {
                    'selectHostGroupRights': value,
                    'selectTemplateGroupRights': value,
                }
            )
            # オプションの変更groups -> host_groups、templategroup追加
            sections['CONFIG_EXPORT'].update(
                {
                    'hostgroup': 'host_groups',
                    'templategroup':'template_groups'
                }
            )
            sections['CONFIG_IMPORT'][6.2] = {}
            sections['CONFIG_IMPORT'][6.2].update(
                {
                    'host_groups': 'hostgroup',
                    'template_groups':'templategroup'
                }
            )
            # インポートルール変更
            # 6.0まで5.0からのインポートに必要なので6.2から不使用にする
            sections['CONFIG_IMPORT'][4.0].pop('value_maps', None)
            value = importRules.pop('groups', None)
            importRules.update(
                {
                    'host_groups': value,
                    'template_groups': value
                }
            )
            discardParameter['authentication']['ldap'].append('ldap_userdirectoryid')

        # 6.4対応
        # 追加Method
        addMethods[6.4] = ['userdirectory']
        if version['major'] >= 6.4:
            # LDAP/SAML対応
            methodParameters.update(
                {
                    'userdirectory': {
                        'id': 'userdirectoryid',
                        'name': 'name',
                        'options': {
                            'output': 'extend',
                            'selectProvisionMedia': 'extend',
                            'selectProvisionGroups': 'extend'
                        }
                    }
                }
            )
            # userでroleidとuserdirectoryidのどちらかが必要になったので追加
            methodParameters['user']['options']['output'].append('userdirectoryid')
            sections['POST'].append('userdirectory')
            # DBダイレクト操作は6.0で無しになったけど、一応名前変更カラムの情報定義
            dbConfigRenameCols.update(
                {
                    6.4: [
                        ('ldap_configured', 'ldap_auth_enabled')
                    ]
                }
            )
            discardParameter['authentication']['ldap'].extend(
                [
                    'ldap_jit_status',
                    'jit_provision_interval',
                ]
            )
            discardParameter['authentication']['saml'].append('saml_jit_status')
            discardParameter['role'].append('services.actions')
            
        # 7.0対応
        # 追加Method
        addMethods[7.0] = ['proxygroup', 'mfa', 'connector']
        if version['major'] >= 7.0:
            # プロキシグループの追加
            # プロキシの設定大幅変更のため入れ替え
            # 認証にMFA追加
            methodParameters.update(
                {
                    'proxygroup': {
                        'id': 'proxy_groupid',
                        'name': 'name',
                        'options': {
                            'output': [
                                'proxy_groupid',
                                'name',
                                'failover_delay',
                                'min_online',
                                'description'
                            ]
                        }
                    },
                    'proxy': {
                        'id': 'proxyid',
                        'name': 'name',
                        'options': {
                            'output': 'extend'
                        }
                    },
                    'mfa': {
                        'id': 'mfaid',
                        'name': 'name',
                        'options': {
                            'output': 'extend'
                        }
                    },
                    'connector': {
                        'id': 'connectorid',
                        'name': 'name',
                        'options': {
                            'output': 'extend',
                            'selectTags': 'extend',
                        }
                    }
                }
            )
            # connectorは他と連携がない
            sections['PRE'].append('connector')
            # proxyより先にproxygroupを処理する
            sections['PRE'].remove('proxy')
            sections['PRE'].append('proxygroup')
            sections['MID'].append('proxy')
            # MFAの方をauthenticationより先に処理する
            sections['POST'].append('mfa')
            # DBダイレクト操作は6.0で無しになったけど、一応廃止カラムの情報定義
            # 認証周りの設定がグローバル設定から削除 -> userdirectory
            dbConfigDropCols.update(
                {
                    7.0: [
                        'ldap_host',
                        'ldap_port',
                        'ldap_base_dn',
                        'ldap_bind_dn',
                        'ldap_bind_password',
                        'ldap_search_attribute',
                        'saml_idp_entityid',
                        'saml_sso_url',
                        'saml_slo_url',
                        'saml_username_attribute',
                        'saml_sp_entityid',
                        'saml_nameid_format',
                        'saml_sign_messages',
                        'saml_sign_assertions',
                        'saml_sign_authn_requests',
                        'saml_sign_logout_requests',
                        'saml_sign_logout_responses',
                        'saml_encrypt_nameid',
                        'saml_encrypt_assertions',
                        'dbversion_status',
                    ]
                }
            )
            # 個別のタイムアウト設定
            timeoutTarget = [
                'simple_check',
                'snmp_agent',
                'external_check',
                'db_monitor',
                'http_agent',
                'ssh_agent',
                'telnet_agent',
                'script',
                'browser'
            ]


        # クラス変数化
        # メソッドget実行のためのパラメータ
        self.methodParameters = methodParameters
        # Configurationでの変換処理実行するなどの区分
        self.sections = sections
        # Configuration.importルール
        self.importRules = importRules
        # メジャーバージョンアップで追加されたメソッド、下位バージョンはこれみてスキップする
        self.addMethods = addMethods
        # DB直接操作、削除されたカラム
        self.dbConfigDropCols = dbConfigDropCols
        # DB直接操作、configテーブルのカラム名変更
        self.dbConfigRenameCols = dbConfigRenameCols
        # メソッド内で除去するパラメーター
        self.discardParameter = discardParameter
        # 7.0対応 アイテム取得のタイムアウト分離
        self.timeoutTarget = timeoutTarget
        # 7.0以降 ZabbixCloudで対応が必要な要素
        self.zabbixCloudSpecialItem = zabbixCloudSpecialItem

        # ID Name->Method変換テーブル生成
        self.idMethod = {}
        for method, parameter in self.methodParameters.items():
            self.idMethod.update(
                {
                    parameter['id']: method
                }
            )
            # テンプレートグループとホストグループでID名が被ってる
            # テンプレートグループを変換で使うことはないのでホストグループ指定に強制
            self.idMethod.update({'groupid': 'hostgroup'})

    def getKeynameInMethod(self, method=None, key='id'):
        '''
        methodParametersからメソッドのID/NAMEキー名を取得する
        '''
        if method not in self.methodParameters.keys():
            return ''
        key = key if key in ['id', 'name'] else 'id'
        return self.methodParameters[method][key]

    def getMethodFromIdname(self, idName=None):
        '''
        ID Nameからメソッド名を返す
        '''
        return self.idMethod.get(idName, None)

