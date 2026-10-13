# Windows desktop shell for the HomeDrive sync executable. Run from its packaged folder.
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[System.Windows.Forms.Application]::EnableVisualStyles()
$client = Join-Path $PSScriptRoot 'homeos-sync.exe'
if (-not (Test-Path -LiteralPath $client -PathType Leaf)) {
    [System.Windows.Forms.MessageBox]::Show('homeos-sync.exe is missing from this folder.', 'HomeDrive Sync') | Out-Null
    exit 1
}
$form = New-Object System.Windows.Forms.Form
$form.Text = 'HomeDrive Sync'
$form.Size = New-Object System.Drawing.Size(780, 650)
$form.MinimumSize = New-Object System.Drawing.Size(700, 560)
$form.StartPosition = 'CenterScreen'

function Add-Label($caption, $top) {
    $control = New-Object System.Windows.Forms.Label
    $control.Text = $caption
    $control.Location = New-Object System.Drawing.Point(16, $top)
    $control.Size = New-Object System.Drawing.Size(115, 24)
    $form.Controls.Add($control)
}
function Add-TextBox($top, $value, $width) {
    $control = New-Object System.Windows.Forms.TextBox
    $control.Location = New-Object System.Drawing.Point(138, $top)
    $control.Size = New-Object System.Drawing.Size($width, 24)
    $control.Text = $value
    $form.Controls.Add($control)
    return $control
}
Add-Label 'Server' 18
$server = Add-TextBox 16 'http://127.0.0.1:8091' 600
Add-Label 'Username' 51
$username = Add-TextBox 49 '' 240
Add-Label 'Password' 84
$password = Add-TextBox 82 '' 240
$password.UseSystemPasswordChar = $true
Add-Label 'Local folder' 117
$folder = Add-TextBox 115 '' 510
$browse = New-Object System.Windows.Forms.Button
$browse.Text = 'Browse…'
$browse.Location = New-Object System.Drawing.Point(655, 113)
$browse.Size = New-Object System.Drawing.Size(83, 28)
$form.Controls.Add($browse)
$browse.Add_Click({
    $dialog = New-Object System.Windows.Forms.FolderBrowserDialog
    $dialog.Description = 'Choose the existing folder to synchronize'
    if ($dialog.ShowDialog() -eq 'OK') { $folder.Text = $dialog.SelectedPath }
})
$syncButton = New-Object System.Windows.Forms.Button
$syncButton.Text = 'Sync now'
$syncButton.Location = New-Object System.Drawing.Point(16, 155)
$syncButton.Size = New-Object System.Drawing.Size(100, 32)
$form.Controls.Add($syncButton)
$refreshButton = New-Object System.Windows.Forms.Button
$refreshButton.Text = 'Refresh conflicts'
$refreshButton.Location = New-Object System.Drawing.Point(128, 155)
$refreshButton.Size = New-Object System.Drawing.Size(130, 32)
$form.Controls.Add($refreshButton)
$folderChoice = New-Object System.Windows.Forms.CheckBox
$folderChoice.Text = 'Selected conflict is a folder (include its contents)'
$folderChoice.Location = New-Object System.Drawing.Point(16, 198)
$folderChoice.Size = New-Object System.Drawing.Size(420, 26)
$form.Controls.Add($folderChoice)
$keepLocal = New-Object System.Windows.Forms.Button
$keepLocal.Text = 'Keep local'
$keepLocal.Location = New-Object System.Drawing.Point(460, 196)
$keepLocal.Size = New-Object System.Drawing.Size(130, 30)
$form.Controls.Add($keepLocal)
$keepRemote = New-Object System.Windows.Forms.Button
$keepRemote.Text = 'Keep remote'
$keepRemote.Location = New-Object System.Drawing.Point(604, 196)
$keepRemote.Size = New-Object System.Drawing.Size(130, 30)
$form.Controls.Add($keepRemote)
$grid = New-Object System.Windows.Forms.DataGridView
$grid.Location = New-Object System.Drawing.Point(16, 236)
$grid.Size = New-Object System.Drawing.Size(718, 205)
$grid.Anchor = 'Top,Left,Right'
$grid.ReadOnly = $true
$grid.AllowUserToAddRows = $false
$grid.AllowUserToDeleteRows = $false
$grid.SelectionMode = 'FullRowSelect'
$grid.MultiSelect = $false
[void]$grid.Columns.Add('Path', 'Conflicting path')
[void]$grid.Columns.Add('Reason', 'Reason')
$grid.Columns[0].Width = 260
$grid.Columns[1].AutoSizeMode = 'Fill'
$form.Controls.Add($grid)
$log = New-Object System.Windows.Forms.TextBox
$log.Location = New-Object System.Drawing.Point(16, 452)
$log.Size = New-Object System.Drawing.Size(718, 150)
$log.Anchor = 'Top,Bottom,Left,Right'
$log.Multiline = $true
$log.ReadOnly = $true
$log.ScrollBars = 'Vertical'
$form.Controls.Add($log)
$script:active = $null
$timer = New-Object System.Windows.Forms.Timer
$timer.Interval = 250
function Set-Busy([bool]$busy) {
    $syncButton.Enabled = -not $busy
    $keepLocal.Enabled = -not $busy
    $keepRemote.Enabled = -not $busy
    $refreshButton.Enabled = -not $busy
}
function Quote-Argument([string]$value) {
    # CommandLineToArgvW quoting: double backslashes before quotes and at end.
    $quoted = '"'
    $slashes = 0
    foreach ($character in $value.ToCharArray()) {
        if ($character -eq '\') { $slashes++; continue }
        if ($character -eq '"') {
            $quoted += ('\' * ($slashes * 2 + 1)) + '"'
        } else {
            $quoted += ('\' * $slashes) + $character
        }
        $slashes = 0
    }
    return $quoted + ('\' * ($slashes * 2)) + '"'
}
function Refresh-Conflicts {
    $grid.Rows.Clear()
    if (-not (Test-Path -LiteralPath $folder.Text -PathType Container)) { return }
    try {
        $json = & $client status $folder.Text --json 2>&1
        if ($LASTEXITCODE -ne 0) { throw [string]::Join("`n", $json) }
        $status = ([string]::Join("`n", $json) | ConvertFrom-Json)
        foreach ($item in $status.conflicts) { [void]$grid.Rows.Add($item.Path, $item.Reason) }
        $pendingCount = if ($null -eq $status.pending) { 0 } else { @($status.pending).Count }
        $conflictCount = if ($null -eq $status.conflicts) { 0 } else { @($status.conflicts).Count }
        $log.AppendText("Pending: $pendingCount; conflicts: $conflictCount.`r`n")
    } catch { $log.AppendText("Status error: $_`r`n") }
}
function Start-Client([string[]]$arguments) {
    if ($script:active) { return }
    if (-not (Test-Path -LiteralPath $folder.Text -PathType Container)) {
        [System.Windows.Forms.MessageBox]::Show('Choose an existing local folder first.', 'HomeDrive Sync') | Out-Null
        return
    }
    if ([string]::IsNullOrWhiteSpace($username.Text) -or [string]::IsNullOrEmpty($password.Text)) {
        [System.Windows.Forms.MessageBox]::Show('Enter your HomeOS username and password.', 'HomeDrive Sync') | Out-Null
        return
    }
    $info = New-Object System.Diagnostics.ProcessStartInfo
    $info.FileName = $client
    $info.UseShellExecute = $false
    $info.RedirectStandardInput = $true
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    $info.CreateNoWindow = $true
    $quoted = foreach ($value in $arguments) { Quote-Argument $value }
    $info.Arguments = [string]::Join(' ', $quoted)
    try {
        $script:active = New-Object System.Diagnostics.Process
        $script:active.StartInfo = $info
        [void]$script:active.Start()
        $script:active.StandardInput.WriteLine($password.Text)
        $script:active.StandardInput.Close()
        $password.Clear()
        Set-Busy $true
        $timer.Start()
        $log.AppendText("Operation started.`r`n")
    } catch {
        $script:active = $null
        Set-Busy $false
        $log.AppendText("Could not start sync: $_`r`n")
    }
}
$timer.Add_Tick({
    if (-not $script:active -or -not $script:active.HasExited) { return }
    $timer.Stop()
    $output = $script:active.StandardOutput.ReadToEnd()
    $failure = $script:active.StandardError.ReadToEnd()
    $code = $script:active.ExitCode
    $script:active.Dispose()
    $script:active = $null
    Set-Busy $false
    if ($output) { $log.AppendText($output + "`r`n") }
    if ($failure) { $log.AppendText($failure + "`r`n") }
    $log.AppendText("Exit code: $code`r`n")
    Refresh-Conflicts
})
$refreshButton.Add_Click({ Refresh-Conflicts })
$syncButton.Add_Click({ Start-Client -arguments @('sync', $folder.Text, '--server', $server.Text, '--user', $username.Text, '--password-stdin') })
function Resolve-Selected([string]$choice) {
    if ($grid.SelectedRows.Count -ne 1) { [System.Windows.Forms.MessageBox]::Show('Select one conflict first.', 'HomeDrive Sync') | Out-Null; return }
    $name = [string]$grid.SelectedRows[0].Cells[0].Value
    $scope = if ($folderChoice.Checked) { "folder and all its contents" } else { 'file' }
    $message = "Keep the $($choice.Replace('keep-', '')) copy of '$name' ($scope)? Displaced content is archived locally or sent to remote Trash."
    if ([System.Windows.Forms.MessageBox]::Show($message, 'Confirm conflict resolution', 'YesNo', 'Warning') -ne 'Yes') { return }
    $arguments = @('resolve', $folder.Text, '--server', $server.Text, '--user', $username.Text, '--password-stdin', '--path', $name, '--choice', $choice)
    if ($folderChoice.Checked) { $arguments += '--folder' }
    Start-Client -arguments $arguments
}
$keepLocal.Add_Click({ Resolve-Selected 'keep-local' })
$keepRemote.Add_Click({ Resolve-Selected 'keep-remote' })
$form.Add_FormClosing({ if ($script:active) { $_.Cancel = $true; [System.Windows.Forms.MessageBox]::Show('Wait for the current sync operation to finish.', 'HomeDrive Sync') | Out-Null } })
[void]$form.ShowDialog()
