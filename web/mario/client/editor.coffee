#
# Copyright (c) 2014 10X Genomics, Inc. All rights reserved.
#
# Angular controllers for mario editor main UI.
#

app = angular.module('app', ['ui.bootstrap'])

# Pipeline graph directive
app.directive('pipeGraph', () ->
    return {
        restrict: 'A',
        scope: { pipelineDec: '=pipelineDec' },
        link: (scope, element, attrs) ->
            scope.$watch(attrs.style, (value) ->
                renderPipeline(scope.pipelineDec)
            )
    }
)
renderPipeline = (pipelineDec) ->
    g = new dagreD3.Digraph() 
    for callStm in pipelineDec.Calls
        g.addNode(callStm.Id, {label: callStm.Id})
    for callStm in pipelineDec.Calls
        for bindStm in callStm.Bindings.List
            valueExp = bindStm.Exp
            if valueExp.Kind == 'call'
                g.addEdge(null, valueExp.Id, callStm.Id, {label:bindStm.Tname})
    (new dagreD3.Renderer()).run(g, d3.select("g#" + pipelineDec.Id))
    d3.selectAll("g.node rect").each((id) ->
        d3.select(this).attr('rx', 20).attr('ry', 20))
    d3.selectAll("g.node").each((id) ->
        d3.select(this).classed('complete', true))

# Main editor directive.
app.directive('mainAceEditor', () ->
    return {
        restrict: 'E'
        scope: { 
            editorInfo: '=editorInfo',
            save: '&save'
        },
        link: (scope, element, attrs) ->
            # Configure main editor with auto-dirty and save and undo key bindings.
            scope.editorInfo.ace = e = initEditor(element[0])
            e.setOptions({ maxLines: 1000 })
            e.on('change', (e) -> scope.$apply(() -> scope.editorInfo.dirty = true))
            e.commands.addCommand({
                name: 'Save',
                bindKey: {win: 'Ctrl-S', mac: 'Command-S'},
                exec: (e) -> scope.save()
                readOnly: false
            })
            e.commands.addCommand({
                name: 'Undo',
                bindKey: {win: 'Ctrl-Z', mac: 'Command-Z'},
                exec: (e) -> e.undo()            
                readOnly: false
            })
    }
)
# Include editor directive.
app.directive('includeAceEditor', () ->
    return {
        restrict: 'E'
        scope: { editorInfo: '=editorInfo' },
        link: (scope, element, attrs) ->
            # Configure include editor with no gutter and read-only.
            scope.editorInfo.ace = e = initEditor(element[0])
            e.renderer.setShowGutter(false)
            e.setReadOnly(true)
    }
)

# Editor helpers.
initEditor = (elem) ->
    editor = ace.edit(elem)
    editor.setTheme('ace/theme/clouds')
    editor.setShowFoldWidgets(false)
    editor.setHighlightActiveLine(false)
    return editor

initSession = (einfo, fname, contents) ->
    session = new ace.EditSession(contents, 'ace/mode/coffee')
    session.setUseWorker(false)
    session.setUndoManager(new ace.UndoManager());
    einfo.ace.setSession(session)
    einfo.dirty = false
    einfo.fname = fname


# Main Controller.
app.controller('MarioEdCtrl', ($scope, $http) ->
    # Define main and include editors.
    $scope.mainEditor = {
        fname: 'select file:',
        dirty: false
    }
    $scope.includeEditor = {
        fname: '',
    }
 
    # File selector.
    $scope.availableFnames = [] 
    $http.get('/files').success((data) -> $scope.availableFnames = data)

    # Event handlers.
    $scope.selectFile = (fname) ->
        $http.post('/load', { fname: fname }).success((data) ->
            # Populate editors with file contents.
            initSession($scope.mainEditor, fname, data.contents)
            initSession($scope.includeEditor, data.includeFname, data.includeContents)

            # Clear compiler messages.
            $scope.compilerMessages = ''

            # Clear the pipeline declaration list (and thus the graph tabs).
            $scope.pipelineDecList = []
        )

    $scope.selectTab = (pipelineDec) ->
        renderPipeline(pipelineDec)

    $scope.build = () ->
        if $scope.mainEditor.fname == 'select file:'
            window.alert('Select a file first.')
            return

        $http.post('/build', { fname: $scope.mainEditor.fname, }).success((data) ->
            if typeof data == 'string'
                # Got an error message, jump to line in main editor.
                locMatch = data.match(/on line (\d+):/)
                if locMatch? then $scope.mainEditor.ace.gotoLine(parseInt(locMatch[1]))

                # Display error, clear graph tabs.
                $scope.compilerMessages = data
                $scope.pipelineDecList = []
            else
                console.log(data)
                # Build was successful.
                ptree = data
                filetypes = _.keys(ptree.FiletypeTable)
                $scope.compilerMessages = "Build successful:\n" +
                    "    #{filetypes.length} filetype declarations\n" +
                    "    #{ptree.Stages.length} stage declarations\n" +
                    "    #{ptree.Pipelines.length} pipeline declarations"
                $scope.pipelineDecList = ptree.Pipelines

                # Switch to tab of first graph.
                if $scope.pipelineDecList[0]?
                    $scope.pipelineDecList[0].active = true
        )
    
    $scope.new = () ->
        fname = window.prompt('Enter file name including .mro extension:')
        if not (fname? and fname.length > 0)
            return

        # Add new filename to selector.
        $scope.availableFnames.push(fname)

        # Clear the editors.
        initSession($scope.mainEditor, fname, '')
        initSession($scope.includeEditor, '', '')
        $scope.mainEditor.dirty = true

        # Clear compiler messages.
        $scope.compileMessages = ''

        # Clear the pipeline declaration list (and thus the graph tabs).
        $scope.pipelineDecList = []

    $scope.save = () ->
        $http.post('/save', { 
                fname: $scope.mainEditor.fname, 
                contents: $scope.mainEditor.ace.getValue() 
            }).success((data) ->            
                $scope.mainEditor.dirty = false
        )
)
