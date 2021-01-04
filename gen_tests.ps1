function build {
    go build -o "ginline.exe"
}

function run($dir) {
    ./ginline.exe $dir
}

build

$tests = (Get-ChildItem -Directory tests).FullName
foreach ($t in $tests) {
    Write-Output $t
    run $t
}
