#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common import *

class ReplicaConfigurationDataMixin:
    def buildConfigurationSections(self):
        '''
        バージョン対応のメソッド-セクション対応dictを生成する
        '''
        sections = {}
        for masterVersion, imports in self.sections['CONFIG_IMPORT'].items():
            # 適用するバージョンより処理バージョンの方が大きければ必要なし
            if masterVersion > self.getLatestVersion('MASTER_VERSION'):
                continue
            else:
                for section, method in imports.items():
                    sections[method] = section
        return sections

    def buildConfigurationImportData(self):
        '''
        STOREデータをconfiguration.import用の大枠へ分類する
        '''
        sections = self.buildConfigurationSections()

        importData = {}
        templates = []
        mediatypes = []
        for method, section in sections.items():
            data = self.STORE.get(method)
            if not data:
                continue
            if method == 'trigger':
                continue
            elif method == 'host':
                # hostは並列処理createで入れるのでスキップ、項目は必要
                importData[section] = []
            elif method == 'template':
                templates = self.normalizeTemplateData(data)
            elif method == 'mediatype':
                for item in data:
                    mediatype = self.normalizeMediatypeData(item['DATA'])
                    mediatypes.append(mediatype)
                importData['media_types'] = sorted(mediatypes, key=lambda x:x['name'])
            else:
                importData[section] = [item['DATA'] for item in data]

        triggers = [trigger['DATA'] for trigger in self.STORE.get('trigger', [])]
        return [importData], templates, triggers

    def normalizeMediatypeData(self, mediatype):
        if mediatype['type'] == 'EMAIL':
            if mediatype.get('provider') and 'RELAY' not in mediatype['provider']:
                mediatype['username'] = mediatype.get('username', 'USERNAME')
                mediatype['password'] = mediatype.get('password', 'PASSWORD')
        if self.VERSION.major >= 6.0:
            # 6.0対応 content-type入りだと失敗するので削除
            if mediatype.get('type') == 'SCRIPT':
                mediatype.pop('content_type', None)
        if self.VERSION.major == 6.4:
            # 6.4対応 SCRIPTが順序データ入りになった
            if mediatype.get('type') == 'SCRIPT':
                idx = 0
                params = []
                for param in mediatype.get('parameters', []):
                    if isinstance(param, str):
                        params.append({'sortorder': str(idx), 'value': param})
                    else:
                        if param.get('sortorder') and param.get('value'):
                            params.append(param)
                    idx += 1
                mediatype.update({'parameters': params})
        if self.VERSION.major >= 7.0:
            # 7.0 content_type完全廃止
            mediatype.pop('content_type', None)
        return mediatype

    def extractConfigurationValueMap(self, importData):
        '''
        valuemap要不要の境界バージョン処理
        '''
        if self.VERSION.major >= 5.4 and self.getLatestVersion('MASTER_VERSION') < 5.4:
            return importData[0].pop('value_maps', None)
        return None
