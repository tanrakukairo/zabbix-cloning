# Disposable live compatibility environment. Always use the explicit context.
param([string]$Context = 't2dev', [string]$Namespace = 'zc-compat-20261107')
$ErrorActionPreference = 'Stop'
$items = @(@{apiVersion='v1';kind='Namespace';metadata=@{name=$Namespace;labels=@{purpose='zc-compatibility-test'}}})
$versions = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'versions.json') -Raw | ConvertFrom-Json
foreach ($release in ($versions.PSObject.Properties | Sort-Object Name)) {
    $version = $release.Name
    $tag = $release.Value
    foreach ($role in @('source','target')) {
        $name = "z$version-$role"
        $dbEnv = @(@{name='POSTGRES_DB';value='zabbix'},@{name='POSTGRES_USER';value='zabbix'},@{name='POSTGRES_PASSWORD';value='zc-disposable-test-only'})
        $appEnv = $dbEnv + @(@{name='DB_SERVER_HOST';value='127.0.0.1'},@{name='ZBX_SERVER_HOST';value='127.0.0.1'},@{name='ZBX_SERVER_NAME';value=$name})
        $containers = @(
            @{name='db';image='postgres:16-alpine';env=$dbEnv;volumeMounts=@(@{name='db';mountPath='/var/lib/postgresql/data'});resources=@{requests=@{cpu='100m';memory='256Mi'};limits=@{memory='1Gi'}}},
            @{name='server';image="zabbix/zabbix-server-pgsql:alpine-$tag";env=$appEnv;resources=@{requests=@{cpu='100m';memory='256Mi'};limits=@{memory='1Gi'}}},
            @{name='web';image="zabbix/zabbix-web-nginx-pgsql:alpine-$tag";env=$appEnv;resources=@{requests=@{cpu='100m';memory='128Mi'};limits=@{memory='512Mi'}};readinessProbe=@{httpGet=@{path='/';port=8080};initialDelaySeconds=15;periodSeconds=10}}
        )
        $items += @{apiVersion='v1';kind='Pod';metadata=@{name=$name;namespace=$Namespace;labels=@{app=$name}};spec=@{containers=$containers;volumes=@(@{name='db';emptyDir=@{}})}}
    }
}
@{apiVersion='v1';kind='List';items=$items} | ConvertTo-Json -Depth 30 | kubectl --context $Context apply -f -
if ($LASTEXITCODE -ne 0) { throw 'Deployment failed' }
