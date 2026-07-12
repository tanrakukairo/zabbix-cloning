#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common import *

class ReplicaConfigurationTemplateMixin:
    def normalizeTemplateData(self, data):
        '''
        テンプレートデータをインポート先バージョン向けに加工する
        '''
        templates = []
        # 6.4 HTTP_AGENT以外に入っている「request_method: POST」を削除
        for item in data:
            template = item['DATA']
            if template.get('items'):
                # 通常アイテム
                for item in template['items']:
                    self.dropUnsupportedRequestMethod(item)
            if template.get('discovery_rules'):
                # LLD
                for rule in template.get('discovery_rules', []):
                    # LLDのアイテム
                    self.dropUnsupportedRequestMethod(rule)
                    if rule.get('item_prototypes'):
                        # アイテムのプロトタイプ
                        for item in rule['item_prototypes']:
                            self.dropUnsupportedRequestMethod(item)
            templates.append(template)
        return sorted(templates, key=lambda x:x['name'])

    def dropUnsupportedRequestMethod(self, item):
        if self.VERSION.major >= 6.4:
            if item.get('type') != 'HTTP_AGENT':
                item.pop('request_method', None)

    def appendTemplateImportData(self, importData, templates, triggers, valueMap):
        '''
        テンプレート依存関係に応じて分割し、configuration.import用データへ追加する
        '''
        templateGroup, groups = self.groupTemplatesByDependency(templates)
        for group in sorted(groups.keys()):
            items = groups[group]
            # インポートエラーが一つでも出ると全部巻き込まれるので、１つずつ入れることにした
            count = 0
            while len(items) > count:
                template = items[count]
                if self.VERSION.major == 4.2:
                    # 4.2だけこれが消えてる
                    self.importRules['templateLinkage'].pop('deleteMissing', None)
                iData = self.buildTemplateImportItem(importData, template, triggers, valueMap)
                importData.append(iData)
                count += 1
        self.templateGroup = templateGroup
        return ZC_COMPLETE

    def groupTemplatesByDependency(self, templates):
        '''
        リンクするテンプレートの依存順にテンプレートを分類する
        '''
        templateGroup = []
        group = 0
        groups = {}
        processed = []
        while templates:
            groups[group] = []
            # グループ０：リンクするテンプレートのない
            # グループ１：グループ０のみリンクしている
            # グループ２：グループ０，１をリンクしている
            # …前グループをリンクしているものがなくなるまで繰り返して分類
            for template in templates.copy():
                # 6.0以前のテンプレートのグループ対応
                if template.get('groups'):
                    templateGroup.extend(template['groups'])
                # ホストのプロトタイプのテンプレートを確認し、processedになければ飛ばす
                ptypeTemplate = []
                for lld in template.get('discovery_rules', []):
                    for ptype in lld.get('host_prototypes', []):
                        ptypeTemplate.extend([item['name'] for item in ptype.get('templates', [])])
                set(ptypeTemplate)
                if not LISTA_ALL_IN_LISTB(ptypeTemplate, processed):
                    continue
                # リンクしているテンプレートが処理済みリストにない
                links = [link['name'] for link in template.get('templates', [])]
                if LISTA_ALL_IN_LISTB(links, processed):
                    # groupに追加
                    groups[group].append(template)
                    # 元リストから消す
                    templates.remove(template)
            # 処理済みに追加
            name = self.getKeynameInMethod('template', 'name')
            processed.extend([template[name] for template in groups[group]])
            # 次のグループ
            group += 1
        return templateGroup, groups

    def buildTemplateImportItem(self, importData, template, triggers, valueMap):
        if self.getLatestVersion('MASTER_VERSION') >= 5.4:
            name = '/%s/' % template['name']
        else:
            name = '{%s:' % template['name']
        iData= {
            'templates': [template],
        }
        # 対象のテンプレート用のTriggersを追加する
        templateTriggers = [trigger for trigger in triggers if name in trigger['expression']]
        if templateTriggers:
            iData.update({'triggers': templateTriggers})
        # ホストプロトタイプのディレクトリ指定がテンプレート内にないとダメっぽいので雑に全部追加
        if self.getLatestVersion('MASTER_VERSION') < 6.0:
            iData.update({'groups': importData[0]['groups']})
        # バージョンが6.0未満だとvalue_mapsがtemplatesに必要
        if self.VERSION.major >= 6.0:
            importData[0].pop('value_maps', None)
        else:
            if valueMap:
                iData.update({'value_maps': valueMap})
            else:
                importData[0].pop('value_maps', None)
        return iData

    def convertTemplateGroups(self, importData):
        '''
        ホストグループとテンプレートグループの分離処理
        '''
        # マスターのバージョンが6.2未満でノード側が6.2以上の場合
        if self.getLatestVersion('MASTER_VERSION') < 6.2 and self.VERSION.major >= 6.2:
            templateGroup = [item['name'] for item in self.templateGroup]
            templateGroup = set(sorted(templateGroup))
            # ホストグループの内templateGroupにあるものは除外
            groups = []
            for group in importData[0]['groups'].copy():
                if group['name'] in templateGroup:
                    continue
                groups.append(group)
            importData[0]['groups'] = groups
            for item in templateGroup:
                if item in self.LOCAL['templategroup'].keys():
                    continue
                try:
                    self.ZAPI.templategroup.create(**{'name': item})
                except Exception as e:
                    self.LOGGER.debug(e)
                    return (False, f'Failed Convert Hostgroup:{item} -> ver.6.2+ Templategroup.')
        return ZC_COMPLETE
